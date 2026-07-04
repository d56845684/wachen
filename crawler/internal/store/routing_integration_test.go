package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ikala/wachen/crawler/internal/adapter"
)

// routingFixture：source → raw → review，回傳 review_id
func routingFixture(t *testing.T, st *Store) string {
	t.Helper()
	ctx := context.Background()
	src := fmt.Sprintf("test_route_%d", time.Now().UnixNano())
	mustExec(t, st, `
		INSERT INTO sources (name, adapter, config, enabled, created_by, updated_by)
		VALUES ($1, 'google_review', '{}', false, 'test:store-integration', 'test:store-integration')`, src)
	res, err := st.InsertRawReviews(ctx, []adapter.RawReview{{
		SourceName: src, ExternalID: "r-1",
		Payload: json.RawMessage(`{"comment": "x"}`), ContentHash: "h1",
		SourceURL: "https://x/1", FetchedAt: time.Now().UTC(),
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	rating := 1.0
	reviewID, _, err := st.UpsertReview(ctx, UpsertReviewParams{
		RawReviewID: res[0].ID, SourceName: src, ExternalID: "r-1",
		Rating: &rating, Content: "x", SourceURL: "https://x/1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return reviewID
}

// mkAnalysis：直接種一筆現行分析（模擬 M4 產出）
func mkAnalysis(t *testing.T, st *Store, reviewID, risk string) string {
	t.Helper()
	ctx := context.Background()
	mustExec(t, st, `UPDATE analysis_results SET is_current = false WHERE review_id = $1 AND is_current`, reviewID)
	var id string
	err := st.Pool.QueryRow(ctx, `
		INSERT INTO analysis_results
		    (review_id, risk_level, sentiment, categories, keywords, risk_reasons,
		     model_name, prompt_version, input_hash, is_current, created_by, updated_by)
		VALUES ($1, $2, 'negative', '{}', '{}', '{}', 'test', 'v-test', $3, true,
		        'test:store-integration', 'test:store-integration')
		RETURNING id`, reviewID, risk, fmt.Sprintf("hash-%d", time.Now().UnixNano())).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func routable(reviewID, analysisID, risk string) *RoutableAnalysis {
	return &RoutableAnalysis{
		AnalysisID: analysisID, ReviewID: reviewID, RiskLevel: risk,
		Summary: "測試摘要", SourceURL: "https://x/1",
	}
}

func countRows(t *testing.T, st *Store, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := st.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// 決策矩陣全路徑：Created → Replay → Acknowledged → Escalated → Reopened
func TestIntegrationRouteCaseMatrix(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	reviewID := routingFixture(t, st)
	now := time.Now()

	lowRule, _ := st.ActiveRule(ctx, "low")
	highRule, _ := st.ActiveRule(ctx, "high")
	if lowRule == nil || highRule == nil {
		t.Fatal("seed routing rules missing")
	}

	// 1) Created：low → 1 個指派（store_manager）+ 1 通知，SLA 48h
	an1 := mkAnalysis(t, st, reviewID, "low")
	caseID, outcome, err := st.RouteCase(ctx, routable(reviewID, an1, "low"), lowRule, now)
	if err != nil || outcome != RouteCreated {
		t.Fatalf("created = %v, %v", outcome, err)
	}
	if n := countRows(t, st, `SELECT count(*) FROM case_assignments WHERE case_id = $1`, caseID); n != 1 {
		t.Errorf("assignments = %d, want 1", n)
	}

	// 2) Replay：同一份分析再來 → 不新增任何東西
	_, outcome, err = st.RouteCase(ctx, routable(reviewID, an1, "low"), lowRule, now)
	if err != nil || outcome != RouteReplay {
		t.Fatalf("replay = %v, %v", outcome, err)
	}
	if n := countRows(t, st, `SELECT count(*) FROM notifications WHERE case_id = $1`, caseID); n != 1 {
		t.Errorf("replay must not add notifications, got %d", n)
	}

	// 3) Acknowledged：開放中、新分析但風險未升（low→low 新的一份）→ 只收指標
	an2 := mkAnalysis(t, st, reviewID, "low")
	_, outcome, err = st.RouteCase(ctx, routable(reviewID, an2, "low"), lowRule, now)
	if err != nil || outcome != RouteAcknowledged {
		t.Fatalf("acknowledged = %v, %v", outcome, err)
	}
	var ptr string
	_ = st.Pool.QueryRow(ctx, `SELECT analysis_id FROM cases WHERE id = $1`, caseID).Scan(&ptr)
	if ptr != an2 {
		t.Error("acknowledged must update analysis_id pointer")
	}

	// 4) Escalated：low 案件遇 high 分析 → 風險/規則/SLA 更新、補指派（1→3 角色）
	an3 := mkAnalysis(t, st, reviewID, "high")
	_, outcome, err = st.RouteCase(ctx, routable(reviewID, an3, "high"), highRule, now)
	if err != nil || outcome != RouteEscalated {
		t.Fatalf("escalated = %v, %v", outcome, err)
	}
	var risk string
	var slaDue time.Time
	_ = st.Pool.QueryRow(ctx, `SELECT risk_level, sla_due_at FROM cases WHERE id = $1`, caseID).Scan(&risk, &slaDue)
	if risk != "high" || slaDue.Sub(now) > 3*time.Hour {
		t.Errorf("escalate: risk=%s sla_in=%v (want high / ~2h)", risk, slaDue.Sub(now))
	}
	if n := countRows(t, st, `SELECT count(*) FROM case_assignments WHERE case_id = $1`, caseID); n != 3 {
		t.Errorf("assignments after escalate = %d, want 3 (dedup store_manager + hq + pr)", n)
	}

	// 5) Reopened：結案後又有新分析 → 狀態回 open、reopened_count++
	mustExec(t, st, `UPDATE cases SET status = 'closed' WHERE id = $1`, caseID)
	an4 := mkAnalysis(t, st, reviewID, "high")
	_, outcome, err = st.RouteCase(ctx, routable(reviewID, an4, "high"), highRule, now)
	if err != nil || outcome != RouteReopened {
		t.Fatalf("reopened = %v, %v", outcome, err)
	}
	var status string
	var reopened int
	_ = st.Pool.QueryRow(ctx, `SELECT status, reopened_count FROM cases WHERE id = $1`, caseID).Scan(&status, &reopened)
	if status != "open" || reopened != 1 {
		t.Errorf("reopen: status=%s count=%d", status, reopened)
	}
}

// 對帳判定式：未認領 → 撿到；路由後 → 消失；再種新分析 → 再撿到
func TestIntegrationFindUnroutedAnalyses(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	reviewID := routingFixture(t, st)
	an := mkAnalysis(t, st, reviewID, "medium")

	contains := func(ids []string, id string) bool {
		for _, v := range ids {
			if v == id {
				return true
			}
		}
		return false
	}
	ids, err := st.FindUnroutedAnalyses(ctx, 0, 1000)
	if err != nil || !contains(ids, reviewID) {
		t.Fatalf("unrouted analysis must be found (err=%v)", err)
	}

	rule, _ := st.ActiveRule(ctx, "medium")
	if _, _, err := st.RouteCase(ctx, routable(reviewID, an, "medium"), rule, time.Now()); err != nil {
		t.Fatal(err)
	}
	ids, _ = st.FindUnroutedAnalyses(ctx, 0, 1000)
	if contains(ids, reviewID) {
		t.Fatal("routed analysis must not be re-flagged")
	}

	// 新分析出現（漏升級情境）→ 再次被撿到
	mkAnalysis(t, st, reviewID, "high")
	ids, _ = st.FindUnroutedAnalyses(ctx, 0, 1000)
	if !contains(ids, reviewID) {
		t.Fatal("new unacknowledged analysis must be re-flagged")
	}
}

// SLA 提醒：逾期 → 排通知一次並標記；不重複
func TestIntegrationDueSLAReminders(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	reviewID := routingFixture(t, st)
	an := mkAnalysis(t, st, reviewID, "high")
	rule, _ := st.ActiveRule(ctx, "high")
	caseID, _, err := st.RouteCase(ctx, routable(reviewID, an, "high"), rule, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	before := countRows(t, st, `SELECT count(*) FROM notifications WHERE case_id = $1`, caseID)

	mustExec(t, st, `UPDATE cases SET sla_due_at = now() - interval '1 hour' WHERE id = $1`, caseID)
	n, err := st.DueSLAReminders(ctx, 50)
	if err != nil || n < 1 {
		t.Fatalf("reminders = %d, %v", n, err)
	}
	after := countRows(t, st, `SELECT count(*) FROM notifications WHERE case_id = $1`, caseID)
	if after != before+2 { // high 案件兩個角色各一則
		t.Errorf("reminder notifications = %d, want %d", after-before, 2)
	}
	// 第二輪不得重複提醒
	if n2, _ := st.DueSLAReminders(ctx, 50); n2 != 0 {
		nOnly := countRows(t, st, `SELECT count(*) FROM cases WHERE id = $1 AND sla_reminded_at IS NOT NULL`, caseID)
		t.Fatalf("second pass reminded %d cases (marked=%d), want 0", n2, nOnly)
	}
}

// 通知生命週期：pending → sent；失敗 3 次 → failed
func TestIntegrationNotificationLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	reviewID := routingFixture(t, st)
	an := mkAnalysis(t, st, reviewID, "low")
	rule, _ := st.ActiveRule(ctx, "low")
	caseID, _, err := st.RouteCase(ctx, routable(reviewID, an, "low"), rule, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	pending, err := st.PendingNotifications(ctx, 1000)
	if err != nil || len(pending) == 0 {
		t.Fatalf("pending = %d, %v", len(pending), err)
	}
	var target PendingNotification
	for _, p := range pending {
		if p.Recipient == "role:store_manager" {
			target = p
		}
	}

	// 失敗 ×3 → failed
	for i := 0; i < 3; i++ {
		if err := st.FinishNotification(ctx, target.ID, fmt.Errorf("smtp down %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	var status string
	var retries int
	_ = st.Pool.QueryRow(ctx,
		`SELECT status, retry_count FROM notifications WHERE id = $1`, target.ID).Scan(&status, &retries)
	if status != "failed" || retries != 3 {
		t.Errorf("status=%s retries=%d, want failed/3", status, retries)
	}
	_ = caseID
}
