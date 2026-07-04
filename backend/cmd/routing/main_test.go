package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikala/wachen/backend/internal/queue"
	"github.com/ikala/wachen/backend/internal/store"
)

type fakeRoutingStore struct {
	analysis   *store.RoutableAnalysis
	rule       *store.RoutingRule
	outcome    store.RouteOutcome
	routeErr   error
	routed     []string
	unrouted   []string
	pending    []store.PendingNotification
	finished   map[string]error
}

func (f *fakeRoutingStore) CurrentAnalysisForRouting(_ context.Context, _ string) (*store.RoutableAnalysis, error) {
	return f.analysis, nil
}
func (f *fakeRoutingStore) ActiveRule(_ context.Context, _ string) (*store.RoutingRule, error) {
	return f.rule, nil
}
func (f *fakeRoutingStore) RouteCase(_ context.Context, a *store.RoutableAnalysis, _ *store.RoutingRule, _ time.Time) (string, store.RouteOutcome, error) {
	if f.routeErr != nil {
		return "", 0, f.routeErr
	}
	f.routed = append(f.routed, a.ReviewID)
	return "case-1", f.outcome, nil
}
func (f *fakeRoutingStore) FindUnroutedAnalyses(_ context.Context, _ time.Duration, _ int) ([]string, error) {
	return f.unrouted, nil
}
func (f *fakeRoutingStore) DueSLAReminders(_ context.Context, _ int) (int, error) { return 0, nil }
func (f *fakeRoutingStore) PendingNotifications(_ context.Context, _ int) ([]store.PendingNotification, error) {
	return f.pending, nil
}
func (f *fakeRoutingStore) FinishNotification(_ context.Context, id string, sendErr error) error {
	if f.finished == nil {
		f.finished = map[string]error{}
	}
	f.finished[id] = sendErr
	return nil
}

type fakeCasePub struct {
	events []queue.CaseEventMsg
	err    error
}

func (f *fakeCasePub) PublishCaseEvent(_ context.Context, m queue.CaseEventMsg) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, m)
	return nil
}

var testLog = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func highAnalysis() *store.RoutableAnalysis {
	return &store.RoutableAnalysis{
		AnalysisID: "an-1", ReviewID: "rev-1", RiskLevel: "high",
		Summary: "食安疑慮", SourceURL: "https://x", StoreName: "一號店",
	}
}

func highRule() *store.RoutingRule {
	return &store.RoutingRule{ID: "rule-h", RiskLevel: "high",
		AssigneeRoles: []string{"hq_service", "pr_legal"}, SLAHours: 2, RequireApproval: true}
}

// happy path：建案 + 發 case.created（帶 analysis_id 冪等鍵與 action）
func TestRouteOneCreates(t *testing.T) {
	st := &fakeRoutingStore{analysis: highAnalysis(), rule: highRule(), outcome: store.RouteCreated}
	pub := &fakeCasePub{}

	if err := routeOne(context.Background(), testLog, st, pub, "rev-1", 1); err != nil {
		t.Fatal(err)
	}
	if len(pub.events) != 1 {
		t.Fatalf("events = %v", pub.events)
	}
	e := pub.events[0]
	if e.CaseID != "case-1" || e.AnalysisID != "an-1" || e.Action != "created" || e.RiskLevel != "high" {
		t.Errorf("event = %+v", e)
	}
}

// Replay 也要發事件——上次可能死在 publish 之前（publish-loss 兜底）
func TestRouteOneReplayRepublishes(t *testing.T) {
	st := &fakeRoutingStore{analysis: highAnalysis(), rule: highRule(), outcome: store.RouteReplay}
	pub := &fakeCasePub{}

	if err := routeOne(context.Background(), testLog, st, pub, "rev-1", 2); err != nil {
		t.Fatal(err)
	}
	if len(pub.events) != 1 || pub.events[0].Action != "replay" {
		t.Fatalf("replay must republish, events = %+v", pub.events)
	}
}

// 已刪除/無分析 → Ack 略過，不建案不發事件
func TestRouteOneNoAnalysisSkips(t *testing.T) {
	st := &fakeRoutingStore{analysis: nil}
	pub := &fakeCasePub{}

	if err := routeOne(context.Background(), testLog, st, pub, "rev-gone", 1); err != nil {
		t.Fatalf("missing analysis must ack, got %v", err)
	}
	if len(st.routed) != 0 || len(pub.events) != 0 {
		t.Error("must not route or publish")
	}
}

// 規則被停用 = 營運設定錯誤 → 回錯誤（重試 + 對帳持續吵，不能靜默漏案件）
func TestRouteOneMissingRuleErrors(t *testing.T) {
	st := &fakeRoutingStore{analysis: highAnalysis(), rule: nil}
	if err := routeOne(context.Background(), testLog, st, &fakeCasePub{}, "rev-1", 1); err == nil {
		t.Fatal("missing rule must be a loud error")
	}
}

// publish 失敗 = 任務失敗 → 重試（下次 Replay 補發）
func TestRouteOnePublishFailureRetries(t *testing.T) {
	st := &fakeRoutingStore{analysis: highAnalysis(), rule: highRule(), outcome: store.RouteCreated}
	pub := &fakeCasePub{err: errors.New("nats down")}
	if err := routeOne(context.Background(), testLog, st, pub, "rev-1", 1); err == nil {
		t.Fatal("publish failure must retry")
	}
}

// 對帳：漏路由的分析全部走同一條 routeOne（單筆失敗不中斷）
func TestReconcileUnrouted(t *testing.T) {
	st := &fakeRoutingStore{
		analysis: highAnalysis(), rule: highRule(), outcome: store.RouteCreated,
		unrouted: []string{"rev-a", "rev-b"},
	}
	pub := &fakeCasePub{}
	n, err := reconcileUnrouted(context.Background(), testLog, st, pub, 2*time.Minute, 20)
	if err != nil || n != 2 || len(pub.events) != 2 {
		t.Fatalf("n=%d err=%v events=%d", n, err, len(pub.events))
	}
}

// Notifier：成功標 sent、失敗記錯誤（FinishNotification 決定 retry/failed）
func TestDeliverNotifications(t *testing.T) {
	st := &fakeRoutingStore{pending: []store.PendingNotification{
		{ID: "n1", Channel: "email", Recipient: "role:hq_service", Subject: "s"},
		{ID: "n2", Channel: "email", Recipient: "role:pr_legal", Subject: "s"},
	}}
	if err := deliverNotifications(context.Background(), testLog, st, logSender{log: testLog}, 50); err != nil {
		t.Fatal(err)
	}
	if len(st.finished) != 2 || st.finished["n1"] != nil || st.finished["n2"] != nil {
		t.Errorf("finished = %v", st.finished)
	}
}

type failingSender struct{}

func (failingSender) Send(store.PendingNotification) error { return errors.New("smtp down") }

func TestDeliverNotificationsRecordsFailure(t *testing.T) {
	st := &fakeRoutingStore{pending: []store.PendingNotification{{ID: "n1", Channel: "email"}}}
	if err := deliverNotifications(context.Background(), testLog, st, failingSender{}, 50); err != nil {
		t.Fatal(err)
	}
	if st.finished["n1"] == nil {
		t.Fatal("send failure must be recorded for retry accounting")
	}
}
