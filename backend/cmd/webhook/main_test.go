package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ikala/wachen/backend/internal/adapter"
	"github.com/ikala/wachen/backend/internal/store"
)

type fakeWebhookStore struct {
	secret    string
	found     bool
	inserted  []adapter.RawReview
	insertRes []store.InsertResult
	insertErr error
}

func (f *fakeWebhookStore) EnabledWebhookSource(_ context.Context, _ string) (string, bool, error) {
	return f.secret, f.found, nil
}
func (f *fakeWebhookStore) InsertRawReviews(_ context.Context, rs []adapter.RawReview, jobID string) ([]store.InsertResult, error) {
	if f.insertErr != nil {
		return nil, f.insertErr
	}
	if jobID != "" {
		return nil, errors.New("webhook must not carry crawl job id")
	}
	f.inserted = append(f.inserted, rs...)
	if f.insertRes != nil {
		return f.insertRes, nil
	}
	return []store.InsertResult{{ID: "raw-w1", Inserted: true}}, nil
}

type fakeRawPub struct {
	published []string
	err       error
}

func (f *fakeRawPub) PublishReviewRaw(_ context.Context, _, id string) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, id)
	return nil
}

var testLog = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func newServer(st *fakeWebhookStore, pub *fakeRawPub) *httptest.Server {
	return httptest.NewServer((&handler{st: st, pub: pub, log: testLog}).routes())
}

const validBody = `{
	"external_id": "web-001", "author": "官網訪客", "rating": 1,
	"content": "訂位系統一直轉圈圈，最後訂不進去",
	"source_url": "https://example.com/feedback/web-001",
	"location_id": "locations/mock-loc-1"
}`

func post(t *testing.T, url, secret, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if secret != "" {
		req.Header.Set("X-Webhook-Secret", secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestWebhookHappyPath(t *testing.T) {
	st := &fakeWebhookStore{secret: "s3cret", found: true}
	pub := &fakeRawPub{}
	ts := newServer(st, pub)
	defer ts.Close()

	resp := post(t, ts.URL+"/v1/sources/webhook_generic/reviews", "s3cret", validBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(st.inserted) != 1 || st.inserted[0].ExternalID != "web-001" {
		t.Errorf("inserted = %+v", st.inserted)
	}
	if st.inserted[0].SourceURL == "" || st.inserted[0].ContentHash == "" {
		t.Error("source_url / content_hash must be set")
	}
	if len(pub.published) != 1 {
		t.Errorf("published = %v", pub.published)
	}
}

func TestWebhookRejectsBadSecret(t *testing.T) {
	st := &fakeWebhookStore{secret: "s3cret", found: true}
	ts := newServer(st, &fakeRawPub{})
	defer ts.Close()

	for _, secret := range []string{"", "wrong"} {
		resp := post(t, ts.URL+"/v1/sources/webhook_generic/reviews", secret, validBody)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("secret %q: status = %d, want 401", secret, resp.StatusCode)
		}
	}
	if len(st.inserted) != 0 {
		t.Error("must not insert on auth failure")
	}
}

// 來源沒設密鑰 = 全拒（不能靜默放行）
func TestWebhookRejectsWhenNoSecretConfigured(t *testing.T) {
	st := &fakeWebhookStore{secret: "", found: true}
	ts := newServer(st, &fakeRawPub{})
	defer ts.Close()

	if resp := post(t, ts.URL+"/v1/sources/webhook_generic/reviews", "", validBody); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// 未知來源與錯誤密鑰同樣回 401——不可洩漏有效來源名（防枚舉）
func TestWebhookUnknownSourceIndistinguishable(t *testing.T) {
	st := &fakeWebhookStore{found: false}
	ts := newServer(st, &fakeRawPub{})
	defer ts.Close()

	if resp := post(t, ts.URL+"/v1/sources/nope/reviews", "x", validBody); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no source enumeration)", resp.StatusCode)
	}
}

// rating 超出 0-10（NPS 上限）要在門口擋下，不能進毒藥迴圈
func TestWebhookRejectsOutOfRangeRating(t *testing.T) {
	st := &fakeWebhookStore{secret: "s", found: true}
	ts := newServer(st, &fakeRawPub{})
	defer ts.Close()

	for _, body := range []string{
		`{"external_id": "e", "source_url": "https://x", "rating": 11}`,
		`{"external_id": "e", "source_url": "https://x", "rating": -1}`,
	} {
		if resp := post(t, ts.URL+"/v1/sources/s/reviews", "s", body); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, resp.StatusCode)
		}
	}
	// NPS 10 分是合法上界
	if resp := post(t, ts.URL+"/v1/sources/s/reviews", "s",
		`{"external_id": "e", "source_url": "https://x", "rating": 10}`); resp.StatusCode != http.StatusCreated {
		t.Errorf("rating=10 must be accepted, got %d", resp.StatusCode)
	}
}

// NUL（jsonb 毒藥）要回 400 而非 500/毒藥迴圈
func TestWebhookRejectsNUL(t *testing.T) {
	st := &fakeWebhookStore{secret: "s", found: true}
	ts := newServer(st, &fakeRawPub{})
	defer ts.Close()

	body := `{"external_id": "e", "source_url": "https://x", "content": "bad\u0000nul"}`
	if resp := post(t, ts.URL+"/v1/sources/s/reviews", "s", body); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// 原文保存：呼叫方多送的未知欄位必須原樣存進 payload，不可被 re-marshal 丟棄
func TestWebhookPreservesRawBody(t *testing.T) {
	st := &fakeWebhookStore{secret: "s", found: true}
	ts := newServer(st, &fakeRawPub{})
	defer ts.Close()

	body := `{"external_id": "e", "source_url": "https://x", "content": "c", "future_field": "must-survive"}`
	if resp := post(t, ts.URL+"/v1/sources/s/reviews", "s", body); resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(st.inserted[0].Payload), "must-survive") {
		t.Fatalf("unknown field dropped from payload: %s", st.inserted[0].Payload)
	}
}

func TestWebhookValidation(t *testing.T) {
	st := &fakeWebhookStore{secret: "s", found: true}
	ts := newServer(st, &fakeRawPub{})
	defer ts.Close()

	cases := map[string]string{
		"缺 external_id":     `{"source_url": "https://x", "content": "c"}`,
		"缺 source_url":      `{"external_id": "e", "content": "c"}`,
		"無 content 也無 rating": `{"external_id": "e", "source_url": "https://x"}`,
		"壞 JSON":            `{{{`,
	}
	for name, body := range cases {
		if resp := post(t, ts.URL+"/v1/sources/s/reviews", "s", body); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, resp.StatusCode)
		}
	}
}

// 冪等重送：同 external_id 同內容 → 200（非 201），仍補發事件
func TestWebhookIdempotentResend(t *testing.T) {
	st := &fakeWebhookStore{secret: "s", found: true,
		insertRes: []store.InsertResult{{ID: "raw-w1", Inserted: false}}}
	pub := &fakeRawPub{}
	ts := newServer(st, pub)
	defer ts.Close()

	resp := post(t, ts.URL+"/v1/sources/s/reviews", "s", validBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(pub.published) != 1 {
		t.Error("resend must still publish (previous publish may have failed)")
	}
}

// 佇列掛掉：回 503 讓對方重送，不能假裝成功
func TestWebhookQueueDown(t *testing.T) {
	st := &fakeWebhookStore{secret: "s", found: true}
	pub := &fakeRawPub{err: errors.New("nats down")}
	ts := newServer(st, pub)
	defer ts.Close()

	if resp := post(t, ts.URL+"/v1/sources/s/reviews", "s", validBody); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}
