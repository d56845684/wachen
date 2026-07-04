package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ikala/wachen/backend/internal/store"
)

type fakeReplyStore struct {
	target     *store.ReplyTarget
	claimErr   error
	sent       *sentRec
	failed     *failedRec
}
type sentRec struct{ extID, url string; platform json.RawMessage }
type failedRec struct{ msg string; final bool }

func (f *fakeReplyStore) ClaimReplyForSend(_ context.Context, _ string) (*store.ReplyTarget, error) {
	return f.target, f.claimErr
}
func (f *fakeReplyStore) MarkReplySent(_ context.Context, _, extID, url string, p json.RawMessage) error {
	f.sent = &sentRec{extID, url, p}
	return nil
}
func (f *fakeReplyStore) MarkReplyFailed(_ context.Context, _, msg string, final bool) error {
	f.failed = &failedRec{msg, final}
	return nil
}

var testLog = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func target(adapter, cfg string) *store.ReplyTarget {
	return &store.ReplyTarget{
		ReplyID: "r1", Content: "感謝您的回饋，已改善", Adapter: adapter,
		Config: json.RawMessage(cfg), ExternalID: "ext-1", CanReply: true,
	}
}

// echo 通道：不打外部、標記送出、記 external_id
func TestSendReplyEchoChannel(t *testing.T) {
	st := &fakeReplyStore{target: target("webhook_generic", `{"reply_channel":"echo"}`)}
	if err := sendReply(context.Background(), testLog, st, "r1", 1, false); err != nil {
		t.Fatal(err)
	}
	if st.sent == nil || st.sent.extID != "ext-1/echo-reply" {
		t.Fatalf("sent = %+v", st.sent)
	}
	if st.failed != nil {
		t.Error("must not mark failed")
	}
}

// callback 通道：POST 到來源系統端點
func TestSendReplyCallbackChannel(t *testing.T) {
	var got map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(200)
	}))
	defer ts.Close()

	cfg := `{"reply_channel":"callback","reply_callback_url":"` + ts.URL + `"}`
	st := &fakeReplyStore{target: target("webhook_generic", cfg)}
	if err := sendReply(context.Background(), testLog, st, "r1", 1, false); err != nil {
		t.Fatal(err)
	}
	if got["reply"] != "感謝您的回饋，已改善" || got["external_id"] != "ext-1" {
		t.Errorf("callback body = %v", got)
	}
	if st.sent == nil {
		t.Error("must mark sent on 2xx")
	}
}

// callback 失敗（5xx）→ 重試（非最後一次投遞 → failed=false 退回 approved）
func TestSendReplyCallbackFailureRetries(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer ts.Close()
	cfg := `{"reply_channel":"callback","reply_callback_url":"` + ts.URL + `"}`
	st := &fakeReplyStore{target: target("webhook_generic", cfg)}

	if err := sendReply(context.Background(), testLog, st, "r1", 2, false); err == nil {
		t.Fatal("want error to trigger retry")
	}
	if st.failed == nil || st.failed.final {
		t.Errorf("should mark failed non-final (retryable), got %+v", st.failed)
	}
}

// can_reply=false → 直接 failed（final），不重試
func TestSendReplyNotAllowed(t *testing.T) {
	tg := target("webhook_generic", `{}`)
	tg.CanReply = false
	st := &fakeReplyStore{target: tg}
	if err := sendReply(context.Background(), testLog, st, "r1", 1, false); err != nil {
		t.Fatalf("must ack (config error), got %v", err)
	}
	if st.failed == nil || !st.failed.final {
		t.Errorf("must mark failed final, got %+v", st.failed)
	}
}

// 狀態不符（已送/被搶）→ Ack 放過
func TestSendReplyBadStateAcks(t *testing.T) {
	st := &fakeReplyStore{claimErr: store.ErrReplyBadState}
	if err := sendReply(context.Background(), testLog, st, "r1", 1, false); err != nil {
		t.Fatalf("bad state must ack, got %v", err)
	}
	if st.sent != nil || st.failed != nil {
		t.Error("must not touch reply on bad state")
	}
}

// 未知 adapter → 送出錯誤
func TestDispatchUnknownAdapter(t *testing.T) {
	_, _, err := dispatch(context.Background(), target("nope", `{}`))
	if err == nil {
		t.Fatal("want error for unknown adapter")
	}
	var _ = errors.New
}
