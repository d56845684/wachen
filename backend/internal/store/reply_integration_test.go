package store

import (
	"context"
	"testing"
	"time"
)

// replyFixture：case + 一筆指定狀態的 reply，回傳 reply id。
// 注意：updated_at 由 touch trigger 維護、無法回填，測試以 olderThan=0 觸發掃描
// （與 TestIntegrationFindStaleNewReviews 同做法）。
func replyFixture(t *testing.T, st *Store, status string, retryCount int) string {
	t.Helper()
	ctx := context.Background()
	reviewID := routingFixture(t, st)
	analysisID := mkAnalysis(t, st, reviewID, "low")
	rule, err := st.ActiveRule(ctx, "low")
	if err != nil {
		t.Fatal(err)
	}
	if rule == nil {
		t.Fatal("no active rule for low risk (seed migration missing?)")
	}
	caseID, _, err := st.RouteCase(ctx, routable(reviewID, analysisID, "low"), rule, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var id string
	err = st.Pool.QueryRow(ctx, `
		INSERT INTO replies (case_id, review_id, content, status, idempotency_key,
		                     retry_count, created_by, updated_by)
		SELECT id, review_id, '測試回覆', $2, gen_random_uuid()::text, $3,
		       'test:store-integration', 'test:store-integration'
		FROM cases WHERE id = $1
		RETURNING id`, caseID, status, retryCount).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func hasID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// sending 卡死 → 退回 approved、retry_count+1、列入重新入列清單
func TestIntegrationReclaimStuckSending(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id := replyFixture(t, st, "sending", 0)

	requeue, err := st.ReclaimStuckReplies(ctx, 0, 4, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !hasID(requeue, id) {
		t.Fatal("stuck sending must be returned for requeue")
	}
	var status string
	var retry int
	if err := st.Pool.QueryRow(ctx,
		`SELECT status, retry_count FROM replies WHERE id = $1`, id).Scan(&status, &retry); err != nil {
		t.Fatal(err)
	}
	if status != "approved" || retry != 1 {
		t.Errorf("status=%s retry=%d, want approved/1", status, retry)
	}
}

// 累計嘗試耗盡 → failed，不再重新入列
func TestIntegrationReclaimExhaustedGoesFailed(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id := replyFixture(t, st, "sending", 4)

	requeue, err := st.ReclaimStuckReplies(ctx, 0, 4, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if hasID(requeue, id) {
		t.Fatal("exhausted reply must NOT be requeued")
	}
	var status string
	if err := st.Pool.QueryRow(ctx,
		`SELECT status FROM replies WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Errorf("status = %s, want failed", status)
	}
}

// 未逾時的 sending 不動（正常送出中不可誤收）
func TestIntegrationReclaimSkipsFreshSending(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id := replyFixture(t, st, "sending", 0)

	requeue, err := st.ReclaimStuckReplies(ctx, time.Hour, 4, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if hasID(requeue, id) {
		t.Fatal("fresh sending must not be reclaimed")
	}
	var status string
	if err := st.Pool.QueryRow(ctx,
		`SELECT status FROM replies WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "sending" {
		t.Errorf("status = %s, want sending untouched", status)
	}
}

// approved 久未消費 → 補發清單；已送出/待審不在其中
func TestIntegrationStaleApprovedReplies(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	approvedID := replyFixture(t, st, "approved", 0)
	sentID := replyFixture(t, st, "sent", 0)

	ids, err := st.StaleApprovedReplies(ctx, 0, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if !hasID(ids, approvedID) {
		t.Fatal("stale approved must be found for republish")
	}
	if hasID(ids, sentID) {
		t.Fatal("sent reply must not be republished")
	}
}

// ClaimReplyForSend 要帶出 idempotency_key（送出路徑防重複發文）
func TestIntegrationClaimCarriesIdempotencyKey(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id := replyFixture(t, st, "approved", 0)

	target, err := st.ClaimReplyForSend(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if target.IdempotencyKey == "" {
		t.Fatal("ReplyTarget.IdempotencyKey must be populated")
	}
}
