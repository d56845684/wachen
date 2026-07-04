package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikala/wachen/crawler/internal/store"
)

type fakeIngestStore struct {
	raw          *store.RawForIngest
	rawErr       error
	upsertErr    error
	outcome      store.UpsertOutcome
	upserted     *store.UpsertReviewParams
	unreflected  []string
	staleNew     []string
	quarantined  []string
	quarantineErr error
	ingestCalled []string
}

func (f *fakeIngestStore) GetRawForIngest(_ context.Context, id string) (*store.RawForIngest, error) {
	f.ingestCalled = append(f.ingestCalled, id)
	if f.rawErr != nil {
		return nil, f.rawErr
	}
	if f.raw == nil {
		return nil, errors.New("no such raw")
	}
	return f.raw, nil
}

func (f *fakeIngestStore) FindUnreflectedRaws(_ context.Context, _ time.Duration, _ int) ([]string, error) {
	return f.unreflected, nil
}

func (f *fakeIngestStore) FindStaleNewReviews(_ context.Context, _ time.Duration, _ int) ([]string, error) {
	return f.staleNew, nil
}

func (f *fakeIngestStore) QuarantineRaw(_ context.Context, id, _ string) error {
	if f.quarantineErr != nil {
		return f.quarantineErr
	}
	f.quarantined = append(f.quarantined, id)
	return nil
}
func (f *fakeIngestStore) UpsertReview(_ context.Context, p store.UpsertReviewParams) (string, store.UpsertOutcome, error) {
	if f.upsertErr != nil {
		return "", 0, f.upsertErr
	}
	f.upserted = &p
	return "rev-1", f.outcome, nil
}

type fakeCreatedPub struct {
	published []string
	err       error
}

func (f *fakeCreatedPub) PublishReviewCreated(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, id)
	return nil
}

var testLog = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func googleRaw() *store.RawForIngest {
	payload, _ := json.Marshal(map[string]any{
		"starRating": "ONE",
		"comment":    "在湯裡吃到頭髮",
		"createTime": "2026-07-04T08:00:00Z",
		"reviewer":   map[string]string{"displayName": "黃淑芬"},
	})
	return &store.RawForIngest{
		ID: "raw-1", SourceName: "google_review_mock_a", Adapter: "google_review",
		ExternalID: "ext-1", Payload: payload,
		SourceURL: "https://search.google.com/local/reviews?placeid=p1",
		LocationID: "locations/mock-loc-1",
	}
}

// happy path：正規化 → upsert → 發 review.created
func TestIngestOneHappyPath(t *testing.T) {
	st := &fakeIngestStore{raw: googleRaw(), outcome: store.UpsertApplied}
	pub := &fakeCreatedPub{}

	if err := ingestOne(context.Background(), testLog, st, pub, "raw-1", 1, false); err != nil {
		t.Fatal(err)
	}
	if len(pub.published) != 1 || pub.published[0] != "rev-1" {
		t.Errorf("published = %v", pub.published)
	}
	if st.upserted.AuthorName != "黃淑芬" || *st.upserted.Rating != 1 {
		t.Errorf("upserted = %+v", st.upserted)
	}
	if st.upserted.LocationID != "locations/mock-loc-1" {
		t.Error("location must flow through for store_id resolution")
	}
}

// 過時版本（亂序事件）：不發事件、不報錯（Ack）
func TestIngestOneStaleSkipsPublish(t *testing.T) {
	st := &fakeIngestStore{raw: googleRaw(), outcome: store.UpsertStale}
	pub := &fakeCreatedPub{}

	if err := ingestOne(context.Background(), testLog, st, pub, "raw-1", 2, false); err != nil {
		t.Fatal(err)
	}
	if len(pub.published) != 0 {
		t.Error("stale version must not publish review.created")
	}
}

// 同版本重放（上次 publish 失敗重試）：必須補發事件
func TestIngestOneReplayRepublishes(t *testing.T) {
	st := &fakeIngestStore{raw: googleRaw(), outcome: store.UpsertReplay}
	pub := &fakeCreatedPub{}

	if err := ingestOne(context.Background(), testLog, st, pub, "raw-1", 2, false); err != nil {
		t.Fatal(err)
	}
	if len(pub.published) != 1 {
		t.Fatal("replay must republish review.created (previous publish may have failed)")
	}
}

// publish 失敗 = 整體失敗 → 佇列重試
func TestIngestOnePublishFailureRetries(t *testing.T) {
	st := &fakeIngestStore{raw: googleRaw(), outcome: store.UpsertApplied}
	pub := &fakeCreatedPub{err: errors.New("nats down")}

	if err := ingestOne(context.Background(), testLog, st, pub, "raw-1", 1, false); err == nil {
		t.Fatal("want error to trigger retry")
	}
}

// 壞 payload：正規化失敗重試無意義 → 記錄後 Ack（回 nil）
func TestIngestOneBadPayloadQuarantines(t *testing.T) {
	raw := googleRaw()
	raw.Payload = []byte(`{{{not json`)
	st := &fakeIngestStore{raw: raw}
	pub := &fakeCreatedPub{}

	if err := ingestOne(context.Background(), testLog, st, pub, "raw-1", 1, false); err != nil {
		t.Fatalf("bad payload must ack, got %v", err)
	}
	if len(pub.published) != 0 {
		t.Error("must not publish for bad payload")
	}
	// 必須進隔離區——否則對帳掃描每輪重撿 = 無限迴圈
	if len(st.quarantined) != 1 || st.quarantined[0] != "raw-1" {
		t.Fatalf("quarantined = %v, want [raw-1]", st.quarantined)
	}
}

// 隔離寫入失敗 → 必須重試（毒藥 raw 不能無蹤消失）
func TestIngestOneQuarantineFailureRetries(t *testing.T) {
	raw := googleRaw()
	raw.Payload = []byte(`{{{not json`)
	st := &fakeIngestStore{raw: raw, quarantineErr: errors.New("db down")}

	if err := ingestOne(context.Background(), testLog, st, &fakeCreatedPub{}, "raw-1", 1, false); err == nil {
		t.Fatal("want error when quarantine fails")
	}
}

// PointerOnly（商家回覆等內容未變）與 Deleted（軟刪除）都不發事件
func TestIngestOnePointerOnlyAndDeletedSkipPublish(t *testing.T) {
	for _, outcome := range []store.UpsertOutcome{store.UpsertPointerOnly, store.UpsertDeleted} {
		st := &fakeIngestStore{raw: googleRaw(), outcome: outcome}
		pub := &fakeCreatedPub{}
		if err := ingestOne(context.Background(), testLog, st, pub, "raw-1", 1, false); err != nil {
			t.Fatalf("outcome %v: %v", outcome, err)
		}
		if len(pub.published) != 0 {
			t.Errorf("outcome %v must not publish review.created", outcome)
		}
	}
}

// raw 讀取失敗（重放競態）→ 重試
func TestIngestOneRawLoadFailureRetries(t *testing.T) {
	st := &fakeIngestStore{rawErr: errors.New("not found yet")}
	if err := ingestOne(context.Background(), testLog, st, &fakeCreatedPub{}, "raw-1", 1, false); err == nil {
		t.Fatal("want error to trigger retry")
	}
}

// upsert 失敗 → 重試
func TestIngestOneUpsertFailureRetries(t *testing.T) {
	st := &fakeIngestStore{raw: googleRaw(), upsertErr: errors.New("db down")}
	if err := ingestOne(context.Background(), testLog, st, &fakeCreatedPub{}, "raw-1", 3, true); err == nil {
		t.Fatal("want error")
	}
}

// 對帳掃描：漏網 raw 全部重跑同一條 ingest 路徑並發事件
func TestReconcileOnceReingestsMissed(t *testing.T) {
	st := &fakeIngestStore{
		raw:         googleRaw(),
		outcome:     store.UpsertApplied,
		unreflected: []string{"raw-lost-1", "raw-lost-2"},
	}
	pub := &fakeCreatedPub{}

	n, _, err := reconcileOnce(context.Background(), testLog, st, pub, 2*time.Minute, 15*time.Minute, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(pub.published) != 2 {
		t.Fatalf("reconciled = %d, published = %d; want 2/2", n, len(pub.published))
	}
	if len(st.ingestCalled) != 2 || st.ingestCalled[0] != "raw-lost-1" {
		t.Errorf("ingested = %v", st.ingestCalled)
	}
}

// 對帳掃描：單筆失敗不中斷整輪，下一輪再試
func TestReconcileOnceContinuesOnFailure(t *testing.T) {
	st := &fakeIngestStore{
		rawErr:      errors.New("db hiccup"),
		unreflected: []string{"raw-a", "raw-b"},
	}
	n, _, err := reconcileOnce(context.Background(), testLog, st, &fakeCreatedPub{}, 2*time.Minute, 15*time.Minute, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("done = %d, want 0", n)
	}
	if len(st.ingestCalled) != 2 {
		t.Errorf("must attempt every id despite failures, got %v", st.ingestCalled)
	}
}

// 無漏網 → 零動作
func TestReconcileOnceNoop(t *testing.T) {
	st := &fakeIngestStore{unreflected: nil}
	pub := &fakeCreatedPub{}
	n, r, err := reconcileOnce(context.Background(), testLog, st, pub, 2*time.Minute, 15*time.Minute, 100)
	if err != nil || n != 0 || r != 0 || len(pub.published) != 0 {
		t.Fatalf("noop expected, got n=%d r=%d err=%v published=%v", n, r, err, pub.published)
	}
}

// 第二條腿：status='new' 卡太久的 reviews 重發 review.created（P1-2 黑洞）
func TestReconcileOnceRepublishesStaleNew(t *testing.T) {
	st := &fakeIngestStore{staleNew: []string{"rev-stuck-1", "rev-stuck-2"}}
	pub := &fakeCreatedPub{}

	_, republished, err := reconcileOnce(context.Background(), testLog, st, pub, 2*time.Minute, 15*time.Minute, 100)
	if err != nil {
		t.Fatal(err)
	}
	if republished != 2 || len(pub.published) != 2 {
		t.Fatalf("republished = %d, events = %v", republished, pub.published)
	}
}
