package webhook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ikala/wachen/backend/internal/adapter"
)

// 主路徑（echo / callback 成功 / callback 5xx）由 cmd/replier 的 sendReply 測試
// 經 registry 蓋到；這裡補設定錯誤路徑。

func TestReplyUnknownChannel(t *testing.T) {
	_, err := New().Reply(context.Background(),
		json.RawMessage(`{"reply_channel":"carrier_pigeon"}`), adapter.ReplyRequest{})
	if err == nil || !strings.Contains(err.Error(), "unknown reply_channel") {
		t.Fatalf("want unknown reply_channel error, got %v", err)
	}
}

func TestReplyCallbackMissingURL(t *testing.T) {
	_, err := New().Reply(context.Background(),
		json.RawMessage(`{"reply_channel":"callback"}`), adapter.ReplyRequest{})
	if err == nil || !strings.Contains(err.Error(), "reply_callback_url missing") {
		t.Fatalf("want missing url error, got %v", err)
	}
}

// 推送型來源不可被排 crawl job
func TestFetchRejected(t *testing.T) {
	if _, err := New().Fetch(context.Background(), adapter.CrawlJob{}); err == nil {
		t.Fatal("Fetch must error: push-based source has nothing to fetch")
	}
}
