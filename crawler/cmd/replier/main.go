// Reply Worker（M7）：消費 reply.requested → 依來源 adapter 送出回覆 → 更新狀態。
//
//	reply.requested ─▶ ClaimReplyForSend（approved→sending，搶占）
//	                    │ can_reply=false → 標記 failed（設定錯誤，不重試）
//	                    ▼
//	              dispatch 送出：
//	                google_review   → GBP v4 reviews.reply（dev 打 mockgoogle）
//	                webhook_generic → reply_channel:
//	                    callback → POST 該來源系統的回覆端點
//	                    echo     → 記錄並視為送出（PoC 模擬店家自有系統回覆）
//	                    ▼
//	              sent（記 external_reply_id）/ failed（重試耗盡）
//
// 對外發文比讀取嚴格：冪等（replies.idempotency_key）+ 全程稽核（svc:replier）。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ikala/wachen/crawler/internal/adapter"
	"github.com/ikala/wachen/crawler/internal/adapter/google"
	"github.com/ikala/wachen/crawler/internal/bootstrap"
	"github.com/ikala/wachen/crawler/internal/store"
)

type replyStore interface {
	ClaimReplyForSend(ctx context.Context, replyID string) (*store.ReplyTarget, error)
	MarkReplySent(ctx context.Context, replyID, externalReplyID, replyURL string, platformResp json.RawMessage) error
	MarkReplyFailed(ctx context.Context, replyID, errMsg string, isFinal bool) error
}

func main() {
	svc := bootstrap.MustInit("replier", "svc:replier")
	defer svc.Close()
	ctx, log, st, q := svc.Ctx, svc.Log, svc.Store, svc.Queue

	cc, err := q.ConsumeReplyRequested(ctx, func(ctx context.Context, replyID string, attempt uint64, isFinal bool) error {
		return sendReply(ctx, log, st, replyID, attempt, isFinal)
	})
	if err != nil {
		log.Error("consume failed", "err", err)
		os.Exit(1)
	}
	defer cc.Stop()

	log.Info("reply worker started")
	<-ctx.Done()
	log.Info("shutting down")
}

func sendReply(ctx context.Context, log *slog.Logger, st replyStore, replyID string, attempt uint64, isFinal bool) error {
	t, err := st.ClaimReplyForSend(ctx, replyID)
	if err != nil {
		// 狀態不符（已送/被搶/被退回）→ Ack 放過，不是錯誤
		if err == store.ErrReplyBadState {
			log.Info("reply not claimable, skip", "reply_id", replyID)
			return nil
		}
		log.Error("claim reply failed", "reply_id", replyID, "attempt", attempt, "err", err)
		return err
	}
	if !t.CanReply {
		_ = st.MarkReplyFailed(ctx, replyID, "source does not support reply", true)
		return nil // 設定錯誤，重試無意義
	}

	res, platform, err := dispatch(ctx, t)
	if err != nil {
		_ = st.MarkReplyFailed(ctx, replyID, err.Error(), isFinal)
		log.Error("reply send failed", "reply_id", replyID, "attempt", attempt, "err", err)
		return fmt.Errorf("send reply: %w", err) // 觸發重試（未達上限時）
	}
	if err := st.MarkReplySent(ctx, replyID, res.ExternalReplyID, res.ReplyURL, platform); err != nil {
		return err
	}
	log.Info("reply sent", "reply_id", replyID, "adapter", t.Adapter, "external_id", res.ExternalReplyID)
	return nil
}

// dispatch 依來源 adapter 送出，回傳 (結果, 平台原始回應)
func dispatch(ctx context.Context, t *store.ReplyTarget) (*adapter.ReplyResult, json.RawMessage, error) {
	switch t.Adapter {
	case "google_review":
		res, err := google.New().Reply(ctx, t.Config, adapter.ReplyRequest{
			ExternalID: t.ExternalID, LocationID: t.LocationID, Content: t.Content,
		})
		if err != nil {
			return nil, nil, err
		}
		return res, json.RawMessage(`{"channel":"gbp_v4"}`), nil

	case "webhook_generic":
		return webhookReply(ctx, t)

	default:
		return nil, nil, fmt.Errorf("adapter %q has no reply channel", t.Adapter)
	}
}

// webhookReply：推送型來源的回覆通道。
//   reply_channel=callback → POST 到 config.reply_callback_url（該來源系統的回覆 API）
//   reply_channel=echo     → PoC 模擬：記錄並視為送出（店家自有系統回覆）
func webhookReply(ctx context.Context, t *store.ReplyTarget) (*adapter.ReplyResult, json.RawMessage, error) {
	var cfg struct {
		Channel     string `json:"reply_channel"`
		CallbackURL string `json:"reply_callback_url"`
	}
	_ = json.Unmarshal(t.Config, &cfg)

	switch cfg.Channel {
	case "callback":
		if cfg.CallbackURL == "" {
			return nil, nil, fmt.Errorf("reply_channel=callback but reply_callback_url missing")
		}
		body, _ := json.Marshal(map[string]string{
			"external_id": t.ExternalID, "reply": t.Content,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.CallbackURL,
			bytes.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return nil, nil, fmt.Errorf("callback status %d", resp.StatusCode)
		}
		return &adapter.ReplyResult{ExternalReplyID: t.ExternalID + "/reply"},
			json.RawMessage(fmt.Sprintf(`{"channel":"callback","status":%d}`, resp.StatusCode)), nil

	case "echo", "":
		// PoC 模擬送出：不打外部，記錄回覆內容供稽核
		return &adapter.ReplyResult{ExternalReplyID: t.ExternalID + "/echo-reply"},
			json.RawMessage(`{"channel":"echo","note":"PoC 模擬回覆已記錄"}`), nil

	default:
		return nil, nil, fmt.Errorf("unknown reply_channel %q", cfg.Channel)
	}
}
