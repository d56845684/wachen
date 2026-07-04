// Routing Engine：review.analyzed → 分流決策 → cases/指派/通知 → case.created。
//
//	review.analyzed（僅提示）──▶ 重讀 is_current 分析（不信 payload）
//	                              │ 無分析/已刪除 → 略過
//	                              ▼
//	                    RouteCase（FOR UPDATE 決策矩陣，見 store/routing.go）
//	                    Created / Escalated / Reopened / Acknowledged / Replay
//	                              ▼
//	                    publish case.created（Replay 也發——publish-loss 兜底）
//
//	背景三迴圈：
//	  對帳（60s）：analyzed-未建案掃描（漏事件/漏升級的最後一道網）
//	  SLA（15s）：逾期未提醒案件 → 排通知（每案一次）
//	  Notifier（10s）：pending 通知 → Sender 送出（PoC: log sender）
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ikala/wachen/crawler/internal/bootstrap"
	"github.com/ikala/wachen/crawler/internal/envutil"
	"github.com/ikala/wachen/crawler/internal/queue"
	"github.com/ikala/wachen/crawler/internal/store"
)

// 依賴以小介面注入（4A 模式），決策路徑全部可用 fake 測試
type routingStore interface {
	CurrentAnalysisForRouting(ctx context.Context, reviewID string) (*store.RoutableAnalysis, error)
	ActiveRule(ctx context.Context, riskLevel string) (*store.RoutingRule, error)
	RouteCase(ctx context.Context, a *store.RoutableAnalysis, rule *store.RoutingRule, now time.Time) (string, store.RouteOutcome, error)
	FindUnroutedAnalyses(ctx context.Context, olderThan time.Duration, limit int) ([]string, error)
	DueSLAReminders(ctx context.Context, limit int) (int, error)
	PendingNotifications(ctx context.Context, limit int) ([]store.PendingNotification, error)
	FinishNotification(ctx context.Context, id string, sendErr error) error
}

type casePublisher interface {
	PublishCaseEvent(ctx context.Context, m queue.CaseEventMsg) error
}

// Sender 是通知發送介面；PoC 用 logSender，M-real 換 SMTP / LINE Messaging API
type Sender interface {
	Send(n store.PendingNotification) error
}

type logSender struct{ log *slog.Logger }

func (s logSender) Send(n store.PendingNotification) error {
	s.log.Info("notification sent (log sender)",
		"channel", n.Channel, "recipient", n.Recipient, "subject", n.Subject)
	return nil
}

func main() {
	svc := bootstrap.MustInit("routing", "svc:routing")
	defer svc.Close()
	ctx, log, st, q := svc.Ctx, svc.Log, svc.Store, svc.Queue

	cc, err := q.ConsumeReviewAnalyzed(ctx, func(ctx context.Context, reviewID string, attempt uint64, isFinal bool) error {
		return routeOne(ctx, log, st, q, reviewID, attempt)
	})
	if err != nil {
		log.Error("consume failed", "err", err)
		os.Exit(1)
	}
	defer cc.Stop()

	var sender Sender
	switch mode := envutil.Or("NOTIFY_MODE", "log"); mode {
	case "log":
		sender = logSender{log: log}
	default:
		log.Error("unknown NOTIFY_MODE", "mode", mode)
		os.Exit(1)
	}

	go loopEvery(ctx, 60*time.Second, func() {
		if n, err := reconcileUnrouted(ctx, log, st, q, 2*time.Minute, 20); err != nil {
			log.Error("routing reconciliation failed", "err", err)
		} else if n > 0 {
			log.Warn("reconciliation routed missed analyses", "count", n)
		}
	})
	go loopEvery(ctx, 15*time.Second, func() {
		if n, err := st.DueSLAReminders(ctx, 50); err != nil {
			log.Error("sla reminder pass failed", "err", err)
		} else if n > 0 {
			log.Warn("sla overdue reminders queued", "count", n)
		}
	})
	go loopEvery(ctx, 10*time.Second, func() {
		if err := deliverNotifications(ctx, log, st, sender, 50); err != nil {
			log.Error("notifier pass failed", "err", err)
		}
	})

	log.Info("routing engine started")
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

func routeOne(ctx context.Context, log *slog.Logger, st routingStore, pub casePublisher,
	reviewID string, attempt uint64) error {

	a, err := st.CurrentAnalysisForRouting(ctx, reviewID)
	if err != nil {
		return err
	}
	if a == nil {
		// 已刪除或分析尚不可見（可見性競態極罕見）——刪除為主因，Ack 即可；
		// 真的漏掉由對帳掃描補（predicate 直接看 DB，不依賴事件）
		log.Info("no routable analysis, skip", "review_id", reviewID)
		return nil
	}
	rule, err := st.ActiveRule(ctx, a.RiskLevel)
	if err != nil {
		return err
	}
	if rule == nil {
		// 規則被停用是營運設定錯誤：重試無用、Ack 會靜默漏案件。
		// 保守路徑：記 ERROR 並重試（對帳也會持續撿起直到規則修復——刻意吵）
		return fmt.Errorf("no enabled routing rule for risk %q; fix routing_rules", a.RiskLevel)
	}
	caseID, outcome, err := st.RouteCase(ctx, a, rule, time.Now())
	if err != nil {
		return fmt.Errorf("route case: %w", err)
	}
	// Replay 也發：上次可能死在 publish 之前（M3/M4 同一教訓，第三次套用）；
	// 下游以 (case_id, analysis_id) 冪等
	if err := pub.PublishCaseEvent(ctx, queue.CaseEventMsg{
		CaseID: caseID, ReviewID: reviewID, AnalysisID: a.AnalysisID,
		RiskLevel: a.RiskLevel, Action: outcome.String(),
	}); err != nil {
		return fmt.Errorf("publish case event: %w", err)
	}
	log.Info("routed", "review_id", reviewID, "case_id", caseID,
		"risk", a.RiskLevel, "outcome", outcome.String(), "attempt", attempt)
	return nil
}

// reconcileUnrouted：analyzed-未建案對帳（每輪上限小，與 ingestion 同哲學）
func reconcileUnrouted(ctx context.Context, log *slog.Logger, st routingStore, pub casePublisher,
	olderThan time.Duration, limit int) (int, error) {

	ids, err := st.FindUnroutedAnalyses(ctx, olderThan, limit)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, id := range ids {
		log.Warn("reconciliation routing missed analysis", "review_id", id)
		if err := routeOne(ctx, log, st, pub, id, 1); err != nil {
			log.Error("reconcile route failed, will retry next pass", "review_id", id, "err", err)
			continue
		}
		done++
	}
	return done, nil
}

func deliverNotifications(ctx context.Context, log *slog.Logger, st routingStore, sender Sender, limit int) error {
	pending, err := st.PendingNotifications(ctx, limit)
	if err != nil {
		return err
	}
	for _, n := range pending {
		sendErr := sender.Send(n)
		if err := st.FinishNotification(ctx, n.ID, sendErr); err != nil {
			return err
		}
		if sendErr != nil {
			log.Error("notification send failed", "id", n.ID, "retry", n.Retry+1, "err", sendErr)
		}
	}
	return nil
}
