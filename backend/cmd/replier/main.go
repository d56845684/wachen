// Reply Worker（M7）：消費 reply.requested → 依來源 adapter 送出回覆 → 更新狀態。
//
//	reply.requested ─▶ ClaimReplyForSend（approved→sending，搶占）
//	                    │ can_reply=false → 標記 failed（設定錯誤，不重試）
//	                    ▼
//	              dispatch 送出：adapter registry 查表 → ReplyCapable type assertion
//	                （新增可回覆來源＝實作 ReplyCapable + registerAdapters 加一行）
//	                google_review   → GBP v4 reviews.reply（dev 打 mockgoogle）
//	                webhook_generic → callback（POST 來源系統）/ echo（PoC 模擬）
//	                    ▼
//	              sent（記 external_reply_id）/ failed（重試耗盡）
//
//	對帳迴圈（60s，與 routing 對帳同哲學——事件會丟，DB 狀態是真相）：
//	  sending 逾時（worker 崩潰於 claim 後）→ 退回 approved 並重新入列
//	  approved 久未消費（入列失敗/publish 遺失）→ 補發 reply.requested
//
// 對外發文比讀取嚴格：冪等（replies.idempotency_key）+ 全程稽核（svc:replier）。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ikala/wachen/backend/internal/adapter"
	"github.com/ikala/wachen/backend/internal/adapter/google"
	"github.com/ikala/wachen/backend/internal/adapter/webhook"
	"github.com/ikala/wachen/backend/internal/bootstrap"
	"github.com/ikala/wachen/backend/internal/queue"
	"github.com/ikala/wachen/backend/internal/store"
)

// registerAdapters：本服務可回覆的來源。新增來源＝實作 adapter.ReplyCapable
// 並在此加一行，dispatch 不用動（ARCHITECTURE §6.1 的 type assertion 設計）。
func registerAdapters() {
	adapter.Register(google.New())
	adapter.Register(webhook.New())
}

type replyStore interface {
	ClaimReplyForSend(ctx context.Context, replyID string) (*store.ReplyTarget, error)
	MarkReplySent(ctx context.Context, replyID, externalReplyID, replyURL string, platformResp json.RawMessage) error
	MarkReplyFailed(ctx context.Context, replyID, errMsg string, isFinal bool) error
	ReclaimStuckReplies(ctx context.Context, stuckFor time.Duration, maxAttempts, limit int) ([]string, error)
	StaleApprovedReplies(ctx context.Context, olderThan time.Duration, limit int) ([]string, error)
}

type replyPublisher interface {
	PublishReplyRequested(ctx context.Context, replyID string) error
}

// 對帳參數：dispatch 單次最長 ~30s（HTTP timeout），5 分鐘未動的 sending 必是卡死；
// approved 3 分鐘涵蓋 MQ 重投遞退避窗口，過早補發無害（Claim 冪等閘門擋住）。
const (
	reconcileInterval  = 60 * time.Second
	stuckSendingAfter  = 5 * time.Minute
	staleApprovedAfter = 3 * time.Minute
	reconcileBatch     = 20
)

func main() {
	registerAdapters()
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

	go loopEvery(ctx, reconcileInterval, func() {
		if err := reconcileReplies(ctx, log, st, q); err != nil {
			log.Error("reply reconciliation failed", "err", err)
		}
	})

	log.Info("reply worker started")
	<-ctx.Done()
	log.Info("shutting down")
}

func loopEvery(ctx context.Context, interval time.Duration, fn func()) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}

// reconcileReplies：回覆鏈路兜底。卡死的 sending 退回 approved 後立即重新入列；
// 久未消費的 approved 補發。重複入列無害（ClaimReplyForSend 只放行一個）；
// publish 失敗只記 log，下一輪對帳自然重撿。
func reconcileReplies(ctx context.Context, log *slog.Logger, st replyStore, pub replyPublisher) error {
	reclaimed, err := st.ReclaimStuckReplies(ctx, stuckSendingAfter, queue.MaxDeliver, reconcileBatch)
	if err != nil {
		return fmt.Errorf("reclaim stuck sending: %w", err)
	}
	for _, id := range reclaimed {
		log.Warn("reclaimed stuck sending reply", "reply_id", id)
	}
	stale, err := st.StaleApprovedReplies(ctx, staleApprovedAfter, reconcileBatch)
	if err != nil {
		return fmt.Errorf("find stale approved: %w", err)
	}
	for _, id := range append(reclaimed, stale...) {
		if err := pub.PublishReplyRequested(ctx, id); err != nil {
			log.Error("republish reply failed, next pass retries", "reply_id", id, "err", err)
			continue
		}
		log.Warn("requeued reply", "reply_id", id)
	}
	return nil
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

	res, err := dispatch(ctx, t)
	if err != nil {
		_ = st.MarkReplyFailed(ctx, replyID, err.Error(), isFinal)
		log.Error("reply send failed", "reply_id", replyID, "attempt", attempt, "err", err)
		return fmt.Errorf("send reply: %w", err) // 觸發重試（未達上限時）
	}
	if err := st.MarkReplySent(ctx, replyID, res.ExternalReplyID, res.ReplyURL, res.Platform); err != nil {
		return err
	}
	log.Info("reply sent", "reply_id", replyID, "adapter", t.Adapter, "external_id", res.ExternalReplyID)
	return nil
}

// dispatch：registry 查 adapter → ReplyCapable type assertion → 送出。
// 兩種失敗都是 sources 設定錯誤（unknown adapter / 不支援回覆），caller 標 failed。
func dispatch(ctx context.Context, t *store.ReplyTarget) (*adapter.ReplyResult, error) {
	ad, ok := adapter.Get(t.Adapter)
	if !ok {
		return nil, fmt.Errorf("unknown adapter %q", t.Adapter)
	}
	rc, ok := ad.(adapter.ReplyCapable)
	if !ok {
		return nil, fmt.Errorf("adapter %q has no reply channel", t.Adapter)
	}
	return rc.Reply(ctx, t.Config, adapter.ReplyRequest{
		ExternalID: t.ExternalID, LocationID: t.LocationID, Content: t.Content,
		IdempotencyKey: t.IdempotencyKey,
	})
}
