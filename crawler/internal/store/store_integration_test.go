package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ikala/wachen/crawler/internal/adapter"
)

// 整合測試：需要真實 PostgreSQL（含 migrations），以 TEST_DATABASE_URL 啟用。
// 執行：make test-integration（連 docker compose 網路）

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; run via `make test-integration`")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	st, err := New(ctx, dsn, "test:store-integration")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Pool.Close() })
	return st
}

// 建立測試專用 source（名稱唯一，避免跨測試互撞）
func createTestSource(t *testing.T, st *Store, cfg string) string {
	t.Helper()
	name := fmt.Sprintf("test_src_%d", time.Now().UnixNano())
	var id string
	err := st.Pool.QueryRow(context.Background(), `
		INSERT INTO sources (name, adapter, config, schedule_cron, enabled, created_by, updated_by)
		VALUES ($1, 'google_review', $2, '* * * * *', false, 'test:store-integration', 'test:store-integration')
		RETURNING id`, name, cfg).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestIntegrationJobLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	srcID := createTestSource(t, st, `{"location_ids": ["locations/test-loc"]}`)
	const loc = "locations/test-loc"

	// 建立任務（per-location 粒度）
	jobID, err := st.CreateJob(ctx, srcID, loc, nil)
	if err != nil {
		t.Fatal(err)
	}
	open, err := st.HasOpenJob(ctx, srcID, loc)
	if err != nil || !open {
		t.Fatalf("HasOpenJob = %v, %v; want true", open, err)
	}
	// 不同 location 不互擋
	otherOpen, err := st.HasOpenJob(ctx, srcID, "locations/other")
	if err != nil || otherOpen {
		t.Fatalf("HasOpenJob(other loc) = %v; want false", otherOpen)
	}

	// 搶占：第一個 worker 成功，第二個必須失敗（分散式不重複執行的關鍵）
	claimed, err := st.ClaimJob(ctx, jobID, "worker-A")
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v; want true", claimed, err)
	}
	claimed2, err := st.ClaimJob(ctx, jobID, "worker-B")
	if err != nil {
		t.Fatal(err)
	}
	if claimed2 {
		t.Fatal("second claim should fail: job already running")
	}

	// 完成任務並寫回 cursor
	cursor := adapter.Cursor{"last_update_time": "2026-07-04T10:00:00Z"}
	if err := st.FinishJob(ctx, jobID, "succeeded", cursor, JobStats{Fetched: 3, Inserted: 3}, ""); err != nil {
		t.Fatal(err)
	}
	open, _ = st.HasOpenJob(ctx, srcID, loc)
	if open {
		t.Error("job finished but HasOpenJob still true")
	}

	// 下一輪任務要拿得到上次的 cursor（增量抓取起點），且限定同 location
	got, err := st.LastSucceededCursor(ctx, srcID, loc)
	if err != nil {
		t.Fatal(err)
	}
	if got["last_update_time"] != "2026-07-04T10:00:00Z" {
		t.Errorf("cursor roundtrip = %v", got)
	}

	// GetJob 帶出 source 設定與 location
	job, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Adapter != "google_review" || job.LocationID != loc {
		t.Errorf("GetJob = %+v", job)
	}
}

// GetJob 透過 stores 表 join 出 place_id（T2-A/T3-A）
func TestIntegrationGetJobResolvesPlaceID(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	loc := fmt.Sprintf("locations/test-place-%d", time.Now().UnixNano())

	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO stores (name, google_location_id, google_place_id, created_by, updated_by)
		VALUES ('測試店', $1, 'place-xyz', 'test:store-integration', 'test:store-integration')`, loc); err != nil {
		t.Fatal(err)
	}
	srcID := createTestSource(t, st, `{}`)
	jobID, err := st.CreateJob(ctx, srcID, loc, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.PlaceID != "place-xyz" {
		t.Errorf("PlaceID = %q, want place-xyz", job.PlaceID)
	}
}

// 版本化冪等（T1-A）：同內容跳過、編輯過的內容成為新版本列
func TestIntegrationInsertRawReviewsVersioned(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	srcID := createTestSource(t, st, `{}`)
	jobID, err := st.CreateJob(ctx, srcID, "locations/v", nil)
	if err != nil {
		t.Fatal(err)
	}

	src := fmt.Sprintf("test_raw_%d", time.Now().UnixNano())
	v1 := adapter.RawReview{
		SourceName: src, ExternalID: "ext-1",
		Payload: json.RawMessage(`{"comment": "難吃", "starRating": "THREE"}`), ContentHash: "hash-v1",
		SourceURL: "https://example.com/r", LocationID: "locations/v", FetchedAt: time.Now().UTC(),
	}
	res, err := st.InsertRawReviews(ctx, []adapter.RawReview{v1}, jobID)
	if err != nil || len(res) != 1 || !res[0].Inserted {
		t.Fatalf("first insert = %+v, %v", res, err)
	}
	firstID := res[0].ID

	// 完全相同的重抓 → 冪等跳過，但仍回傳既有 id（2A 補發事件用）
	res, err = st.InsertRawReviews(ctx, []adapter.RawReview{v1}, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Inserted || res[0].ID != firstID {
		t.Fatalf("identical refetch = %+v; want skipped with same id", res[0])
	}

	// 編輯過的評論（同 external_id、新 hash）→ 新版本列
	v2 := v1
	v2.Payload = json.RawMessage(`{"comment": "難吃，吃完食物中毒", "starRating": "ONE"}`)
	v2.ContentHash = "hash-v2"
	res, err = st.InsertRawReviews(ctx, []adapter.RawReview{v2}, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if !res[0].Inserted || res[0].ID == firstID {
		t.Fatalf("edited review must be a new version row, got %+v", res[0])
	}

	var versions int
	if err := st.Pool.QueryRow(ctx, `
		SELECT count(*) FROM raw_reviews WHERE source_name = $1 AND external_id = 'ext-1'`, src).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 2 {
		t.Errorf("versions = %d, want 2", versions)
	}

	// 稽核鏈：兩個版本的寫入都在 audit_logs（trigger 已改 SECURITY DEFINER）
	var audited int
	if err := st.Pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE table_name = 'raw_reviews' AND changed_by = 'test:store-integration'
		  AND new_data->>'source_name' = $1`, src).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if audited != 2 {
		t.Errorf("audit_logs entries = %d, want 2", audited)
	}

	if err := st.FinishJob(ctx, jobID, "succeeded", nil, JobStats{}, ""); err != nil {
		t.Fatal(err)
	}
}

// 1A + 外部聲音：reaper 回收卡死的 running 與孤兒 pending
func TestIntegrationReapStaleJobs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	srcID := createTestSource(t, st, `{}`)

	// 卡死的 running（started_at 拉到過去）
	stuckID, err := st.CreateJob(ctx, srcID, "locations/stuck", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.ClaimJob(ctx, stuckID, "dead-worker"); !ok {
		t.Fatal("claim failed")
	}
	// 孤兒 pending（created_at 拉到過去）
	orphanID, err := st.CreateJob(ctx, srcID, "locations/orphan", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 健康的 pending（不該被回收）
	healthyID, err := st.CreateJob(ctx, srcID, "locations/healthy", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, st, `UPDATE crawl_jobs SET started_at = now() - interval '10 minutes' WHERE id = $1`, stuckID)
	mustExec(t, st, `UPDATE crawl_jobs SET created_at = now() - interval '20 minutes' WHERE id = $1`, orphanID)

	reaped, err := st.ReapStaleJobs(ctx, 5*time.Minute, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if reaped < 2 {
		t.Fatalf("reaped = %d, want >= 2", reaped)
	}
	for id, want := range map[string]string{stuckID: "failed", orphanID: "failed", healthyID: "pending"} {
		var status string
		if err := st.Pool.QueryRow(ctx, `SELECT status FROM crawl_jobs WHERE id = $1`, id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != want {
			t.Errorf("job %s status = %s, want %s", id, status, want)
		}
	}
	// 回收後 HasOpenJob 釋放 → cron 可重排
	if open, _ := st.HasOpenJob(ctx, srcID, "locations/stuck"); open {
		t.Error("reaped job must release HasOpenJob")
	}
	// 清場：把 healthy 收掉，避免殘留 pending 之後被 reaper 撿走干擾 e2e 檢查
	_ = st.FinishJob(ctx, healthyID, "succeeded", nil, JobStats{}, "")
}

// EnabledSources 過濾：disabled / 軟刪除 / 無 cron 的來源都不參與排程
func TestIntegrationEnabledSourcesFiltering(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	mustExec(t, st, fmt.Sprintf(`
		INSERT INTO sources (name, adapter, config, schedule_cron, enabled, created_by, updated_by) VALUES
		('test_on_%d',      'google_review', '{}', '* * * * *', true,  'test:store-integration', 'test:store-integration'),
		('test_off_%d',     'google_review', '{}', '* * * * *', false, 'test:store-integration', 'test:store-integration'),
		('test_nocron_%d',  'google_review', '{}', NULL,        true,  'test:store-integration', 'test:store-integration')`,
		suffix, suffix, suffix))
	mustExec(t, st, fmt.Sprintf(`
		INSERT INTO sources (name, adapter, config, schedule_cron, enabled, deleted_at, created_by, updated_by)
		VALUES ('test_del_%d', 'google_review', '{}', '* * * * *', true, now(), 'test:store-integration', 'test:store-integration')`, suffix))

	sources, err := st.EnabledSources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, s := range sources {
		found[s.Name] = true
	}
	if !found[fmt.Sprintf("test_on_%d", suffix)] {
		t.Error("enabled source missing")
	}
	for _, bad := range []string{"test_off_", "test_nocron_", "test_del_"} {
		if found[fmt.Sprintf("%s%d", bad, suffix)] {
			t.Errorf("%s* must be filtered out", bad)
		}
	}
	// 清場
	mustExec(t, st, `UPDATE sources SET enabled = false WHERE name LIKE 'test_on_%'`)
}

func TestIntegrationLeaderLock(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	const key = 990001 // 測試專用 key，避開 scheduler 的 823001

	lock1, ok1, err := st.AcquireLeaderLock(ctx, key)
	if err != nil || !ok1 {
		t.Fatalf("first acquire = %v, %v; want true", ok1, err)
	}
	// 心跳：健康連線 Ping 應成功
	if err := lock1.Ping(ctx); err != nil {
		t.Fatalf("heartbeat on healthy lock: %v", err)
	}
	// 同一個 key 第二次取鎖（不同連線）必須失敗 → standby
	lock2, ok2, err := st.AcquireLeaderLock(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if ok2 {
		lock2.Release()
		t.Fatal("second acquire should fail while lock held")
	}
	// 釋放後可重新取得
	lock1.Release()
	lock3, ok3, err := st.AcquireLeaderLock(ctx, key)
	if err != nil || !ok3 {
		t.Fatalf("re-acquire after release = %v, %v; want true", ok3, err)
	}
	lock3.Release()
}

func mustExec(t *testing.T, st *Store, sql string, args ...any) {
	t.Helper()
	if _, err := st.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}
