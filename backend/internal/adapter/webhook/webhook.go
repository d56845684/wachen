// Package webhook：推送型來源（官網/APP 留言、客服管道）的 adapter。
// 資料由 Webhook Gateway 推入，Fetch 不適用；Reply 依 sources.config 的
// reply_channel 決定通道：
//
//	callback → POST 到 config.reply_callback_url（該來源系統的回覆 API）
//	echo     → PoC 模擬：不打外部，記錄並視為送出
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ikala/wachen/backend/internal/adapter"
)

type Adapter struct {
	HTTP *http.Client
}

func New() *Adapter {
	return &Adapter{HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (a *Adapter) Name() string { return "webhook_generic" }

// Fetch：推送型來源沒有抓取語意——被排到 crawl job 即是 sources 設定錯誤
func (a *Adapter) Fetch(_ context.Context, _ adapter.CrawlJob) (*adapter.FetchResult, error) {
	return nil, errors.New("webhook_generic is push-based; nothing to fetch")
}

func (a *Adapter) Reply(ctx context.Context, rawCfg json.RawMessage, req adapter.ReplyRequest) (*adapter.ReplyResult, error) {
	var cfg struct {
		Channel     string `json:"reply_channel"`
		CallbackURL string `json:"reply_callback_url"`
	}
	_ = json.Unmarshal(rawCfg, &cfg)

	switch cfg.Channel {
	case "callback":
		if cfg.CallbackURL == "" {
			return nil, fmt.Errorf("reply_channel=callback but reply_callback_url missing")
		}
		// idempotency_key 一併帶出：對帳補送/重投遞時，來源系統可據此去重
		body, _ := json.Marshal(map[string]string{
			"external_id": req.ExternalID, "reply": req.Content,
			"idempotency_key": req.IdempotencyKey,
		})
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.CallbackURL,
			bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := a.HTTP.Do(httpReq)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("callback status %d", resp.StatusCode)
		}
		return &adapter.ReplyResult{
			ExternalReplyID: req.ExternalID + "/reply",
			Platform:        json.RawMessage(fmt.Sprintf(`{"channel":"callback","status":%d}`, resp.StatusCode)),
		}, nil

	case "echo", "":
		// PoC 模擬送出：不打外部，記錄回覆內容供稽核
		return &adapter.ReplyResult{
			ExternalReplyID: req.ExternalID + "/echo-reply",
			Platform:        json.RawMessage(`{"channel":"echo","note":"PoC 模擬回覆已記錄"}`),
		}, nil

	default:
		return nil, fmt.Errorf("unknown reply_channel %q", cfg.Channel)
	}
}
