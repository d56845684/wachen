// Crawler Worker：無狀態、可水平擴展。
// 從 NATS 消費 crawl job → 搶占（ClaimJob）→ 執行 adapter Fetch →
// 整批冪等寫入 raw_reviews → 每列發 review.raw（含既有版本，2A）→ 回寫任務結果。
//
//	runJob 錯誤語意：
//	  claim 輸掉        → nil（他人已處理，Ack）
//	  unknown adapter   → dead_letter + nil（重試無意義，Ack）
//	  fetch/insert/publish 失敗 → failed + error（NATS 退避重試；
//	                              最後一次投遞改標 dead_letter）
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ikala/wachen/crawler/internal/adapter"
	"github.com/ikala/wachen/crawler/internal/adapter/google"
	"github.com/ikala/wachen/crawler/internal/bootstrap"
	"github.com/ikala/wachen/crawler/internal/store"
)

// runJob 的依賴以小介面注入（4A）——*store.Store / *queue.Queue 天然滿足，
// 測試用 fake 打穿全部錯誤路徑
type jobStore interface {
	GetJob(ctx context.Context, jobID string) (*adapter.CrawlJob, error)
	ClaimJob(ctx context.Context, jobID, workerID string) (bool, error)
	FinishJob(ctx context.Context, jobID, status string, cursor adapter.Cursor, stats store.JobStats, errMsg string) error
	InsertRawReviews(ctx context.Context, reviews []adapter.RawReview, jobID string) ([]store.InsertResult, error)
}

type eventPublisher interface {
	PublishReviewRaw(ctx context.Context, sourceName, rawReviewID string) error
}

func main() {
	workerID := os.Getenv("WORKER_ID")
	if workerID == "" {
		workerID, _ = os.Hostname()
	}
	adapter.Register(google.New())

	svc := bootstrap.MustInit("crawler-worker", "svc:crawler-worker:"+workerID)
	defer svc.Close()
	ctx, st, q := svc.Ctx, svc.Store, svc.Queue
	log := svc.Log.With("worker_id", workerID)

	cc, err := q.ConsumeCrawlJobs(ctx, func(ctx context.Context, jobID string, attempt uint64, isFinal bool) error {
		return runJob(ctx, log, st, q, workerID, jobID, attempt, isFinal)
	})
	if err != nil {
		log.Error("consume failed", "err", err)
		os.Exit(1)
	}
	defer cc.Stop()

	log.Info("worker started")
	<-ctx.Done()
	log.Info("shutting down")
}

func runJob(ctx context.Context, log *slog.Logger, st jobStore, pub eventPublisher,
	workerID, jobID string, attempt uint64, isFinal bool) error {

	job, err := st.GetJob(ctx, jobID)
	if err != nil {
		log.Error("load job failed", "job_id", jobID, "err", err)
		return err
	}
	claimed, err := st.ClaimJob(ctx, jobID, workerID)
	if err != nil {
		return err
	}
	if !claimed {
		log.Info("job already taken, skip", "job_id", jobID)
		return nil // 其他 worker 已處理，直接 Ack
	}

	failStatus := "failed"
	if isFinal {
		failStatus = "dead_letter"
	}

	ad, ok := adapter.Get(job.Adapter)
	if !ok {
		_ = st.FinishJob(ctx, jobID, "dead_letter", job.Cursor, store.JobStats{}, "unknown adapter: "+job.Adapter)
		return nil // 設定錯誤，重試無意義
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	res, err := ad.Fetch(fetchCtx, *job)
	if err != nil {
		_ = st.FinishJob(ctx, jobID, failStatus, job.Cursor, store.JobStats{}, err.Error())
		log.Error("fetch failed", "job_id", jobID, "source", job.SourceName, "attempt", attempt, "err", err)
		return fmt.Errorf("fetch: %w", err)
	}
	if res.PageCapHit {
		// 3A：首次同步截斷必須可見——營運查 stats 就知道哪個門市要人工處理
		log.Warn("page cap hit: oldest reviews not fetched",
			"job_id", jobID, "source", job.SourceName, "location", job.LocationID)
	}

	stats := store.JobStats{Fetched: len(res.Reviews), PageCapHit: res.PageCapHit}
	results, err := st.InsertRawReviews(ctx, res.Reviews, jobID)
	if err != nil {
		_ = st.FinishJob(ctx, jobID, failStatus, job.Cursor, stats, err.Error())
		return fmt.Errorf("insert raw reviews: %w", err)
	}
	// 2A：新列與既有版本都發事件——重試場景下游才不會漏；
	// M3 以 raw_review_id 冪等，重複事件無害。publish 失敗 = 任務失敗（觸發重試）。
	for i, r := range results {
		if r.Inserted {
			stats.Inserted++
		} else {
			stats.Duplicates++
		}
		if err := pub.PublishReviewRaw(ctx, res.Reviews[i].SourceName, r.ID); err != nil {
			_ = st.FinishJob(ctx, jobID, failStatus, job.Cursor, stats, "publish review.raw: "+err.Error())
			return fmt.Errorf("publish review.raw: %w", err)
		}
	}
	if err := st.FinishJob(ctx, jobID, "succeeded", res.Cursor, stats, ""); err != nil {
		return err
	}
	log.Info("job done", "job_id", jobID, "source", job.SourceName, "location", job.LocationID,
		"fetched", stats.Fetched, "inserted", stats.Inserted, "duplicates", stats.Duplicates)
	return nil
}
