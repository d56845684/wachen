package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/ikala/wachen/crawler/internal/adapter"
	"github.com/ikala/wachen/crawler/internal/store"
)

// ---- fakes ----

type fakeStore struct {
	job        *adapter.CrawlJob
	claimOK    bool
	claimErr   error
	insertErr  error
	insertRes  []store.InsertResult
	finalState string
	finalErr   string
	finalStats store.JobStats
}

func (f *fakeStore) GetJob(_ context.Context, _ string) (*adapter.CrawlJob, error) {
	if f.job == nil {
		return nil, errors.New("no such job")
	}
	return f.job, nil
}
func (f *fakeStore) ClaimJob(_ context.Context, _, _ string) (bool, error) {
	return f.claimOK, f.claimErr
}
func (f *fakeStore) FinishJob(_ context.Context, _, status string, _ adapter.Cursor, stats store.JobStats, errMsg string) error {
	f.finalState, f.finalErr, f.finalStats = status, errMsg, stats
	return nil
}
func (f *fakeStore) InsertRawReviews(_ context.Context, reviews []adapter.RawReview, _ string) ([]store.InsertResult, error) {
	if f.insertErr != nil {
		return nil, f.insertErr
	}
	if f.insertRes != nil {
		return f.insertRes, nil
	}
	out := make([]store.InsertResult, len(reviews))
	for i := range reviews {
		out[i] = store.InsertResult{ID: "raw-" + reviews[i].ExternalID, Inserted: true}
	}
	return out, nil
}

type fakePublisher struct {
	published []string
	failAfter int // 發佈 N 次後開始失敗；-1 = 永不失敗
}

func (f *fakePublisher) PublishReviewRaw(_ context.Context, _, id string) error {
	if f.failAfter >= 0 && len(f.published) >= f.failAfter {
		return errors.New("nats down")
	}
	f.published = append(f.published, id)
	return nil
}

type fakeAdapter struct {
	res *adapter.FetchResult
	err error
}

func (f *fakeAdapter) Name() string { return "fake_adapter" }
func (f *fakeAdapter) Fetch(_ context.Context, _ adapter.CrawlJob) (*adapter.FetchResult, error) {
	return f.res, f.err
}

// ---- helpers ----

var testLog = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func jobFor(adapterName string) *adapter.CrawlJob {
	return &adapter.CrawlJob{ID: "j1", SourceName: "src", Adapter: adapterName, LocationID: "locations/l1"}
}

func twoReviews() *adapter.FetchResult {
	return &adapter.FetchResult{
		Reviews: []adapter.RawReview{
			{ExternalID: "a", SourceName: "src"},
			{ExternalID: "b", SourceName: "src"},
		},
		Cursor: adapter.Cursor{"last_update_time": "2026-07-04T10:00:00Z"},
	}
}

// ---- tests ----

// happy path：全部入庫、全部發事件、任務 succeeded、stats 正確
func TestRunJobHappyPath(t *testing.T) {
	fa := &fakeAdapter{res: twoReviews()}
	adapter.Register(fa)
	st := &fakeStore{job: jobFor(fa.Name()), claimOK: true}
	pub := &fakePublisher{failAfter: -1}

	if err := runJob(context.Background(), testLog, st, pub, "w1", "j1", 1, false); err != nil {
		t.Fatal(err)
	}
	if st.finalState != "succeeded" {
		t.Errorf("state = %s", st.finalState)
	}
	if st.finalStats.Inserted != 2 || st.finalStats.Fetched != 2 {
		t.Errorf("stats = %+v", st.finalStats)
	}
	if len(pub.published) != 2 {
		t.Errorf("published %d events, want 2", len(pub.published))
	}
}

// claim 輸掉：他人已處理 → 回 nil（Ack），不能動任務狀態
func TestRunJobClaimLost(t *testing.T) {
	fa := &fakeAdapter{res: twoReviews()}
	adapter.Register(fa)
	st := &fakeStore{job: jobFor(fa.Name()), claimOK: false}
	pub := &fakePublisher{failAfter: -1}

	if err := runJob(context.Background(), testLog, st, pub, "w1", "j1", 2, false); err != nil {
		t.Fatalf("claim-lost must return nil (ack), got %v", err)
	}
	if st.finalState != "" {
		t.Errorf("must not touch job state, got %s", st.finalState)
	}
	if len(pub.published) != 0 {
		t.Error("must not publish anything")
	}
}

// fetch 錯誤：任務 failed + 回傳 error（觸發 NATS 重試）
func TestRunJobFetchError(t *testing.T) {
	fa := &fakeAdapter{err: errors.New("api 429")}
	adapter.Register(fa)
	st := &fakeStore{job: jobFor(fa.Name()), claimOK: true}

	err := runJob(context.Background(), testLog, st, &fakePublisher{failAfter: -1}, "w1", "j1", 1, false)
	if err == nil {
		t.Fatal("want error to trigger retry")
	}
	if st.finalState != "failed" {
		t.Errorf("state = %s, want failed", st.finalState)
	}
}

// 最後一次投遞的失敗 → dead_letter（不再重試）
func TestRunJobFinalDeliveryDeadLetters(t *testing.T) {
	fa := &fakeAdapter{err: errors.New("api down")}
	adapter.Register(fa)
	st := &fakeStore{job: jobFor(fa.Name()), claimOK: true}

	if err := runJob(context.Background(), testLog, st, &fakePublisher{failAfter: -1}, "w1", "j1", 4, true); err == nil {
		t.Fatal("want error")
	}
	if st.finalState != "dead_letter" {
		t.Errorf("state = %s, want dead_letter", st.finalState)
	}
}

// publish 失敗 = 任務失敗（2A：事件不能靜默丟失）
func TestRunJobPublishFailureFailsJob(t *testing.T) {
	fa := &fakeAdapter{res: twoReviews()}
	adapter.Register(fa)
	st := &fakeStore{job: jobFor(fa.Name()), claimOK: true}
	pub := &fakePublisher{failAfter: 1} // 第 2 次 publish 失敗

	if err := runJob(context.Background(), testLog, st, pub, "w1", "j1", 1, false); err == nil {
		t.Fatal("want error when publish fails")
	}
	if st.finalState != "failed" {
		t.Errorf("state = %s, want failed", st.finalState)
	}
}

// 2A：既有版本（duplicate）也要補發事件——重試場景下游才不會漏
func TestRunJobRepublishesDuplicates(t *testing.T) {
	fa := &fakeAdapter{res: twoReviews()}
	adapter.Register(fa)
	st := &fakeStore{
		job: jobFor(fa.Name()), claimOK: true,
		insertRes: []store.InsertResult{
			{ID: "raw-a", Inserted: false}, // 上次崩潰前已入庫
			{ID: "raw-b", Inserted: true},
		},
	}
	pub := &fakePublisher{failAfter: -1}

	if err := runJob(context.Background(), testLog, st, pub, "w1", "j1", 2, false); err != nil {
		t.Fatal(err)
	}
	if len(pub.published) != 2 {
		t.Fatalf("duplicates must republish: got %d events, want 2", len(pub.published))
	}
	if st.finalStats.Duplicates != 1 || st.finalStats.Inserted != 1 {
		t.Errorf("stats = %+v", st.finalStats)
	}
}

// unknown adapter：設定錯誤 → dead_letter + nil（重試無意義）
func TestRunJobUnknownAdapter(t *testing.T) {
	st := &fakeStore{job: jobFor("no_such_adapter"), claimOK: true}

	if err := runJob(context.Background(), testLog, st, &fakePublisher{failAfter: -1}, "w1", "j1", 1, false); err != nil {
		t.Fatalf("config error must ack (nil), got %v", err)
	}
	if st.finalState != "dead_letter" {
		t.Errorf("state = %s, want dead_letter", st.finalState)
	}
}

// PageCapHit 要進 stats（3A：截斷可見）
func TestRunJobRecordsPageCapHit(t *testing.T) {
	res := twoReviews()
	res.PageCapHit = true
	fa := &fakeAdapter{res: res}
	adapter.Register(fa)
	st := &fakeStore{job: jobFor(fa.Name()), claimOK: true}

	if err := runJob(context.Background(), testLog, st, &fakePublisher{failAfter: -1}, "w1", "j1", 1, false); err != nil {
		t.Fatal(err)
	}
	if !st.finalStats.PageCapHit {
		t.Error("stats.PageCapHit must be recorded")
	}
}
