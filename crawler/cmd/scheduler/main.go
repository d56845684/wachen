// Scheduler：讀 sources 設定，依 cron 為每個 source×location 產生 crawl_jobs 並推進 NATS。
// 以 PG advisory lock 選主 — 可跑多個 replica，只有 leader 派工，避免重複。
//
//	任務生命週期（含 reaper）：
//
//	[pending] ──Claim──▶ [running] ──Finish──▶ [succeeded]
//	    │                    │  │
//	    │                    │  └─錯誤/publish失敗─▶ [failed] ─重試─▶ Claim
//	    │                    │                          │
//	    │                    │                          └─達上限─▶ [dead_letter]
//	    │                    └─worker 死亡 ▶ 卡 running ─┐
//	    └─publish 失敗 ▶ 孤兒 pending ────────────────────┤
//	                                                     ▼
//	                                        reaper（leader 每 tick）
//	                                        超時 → failed → cron 自然重排
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/ikala/wachen/crawler/internal/bootstrap"
	"github.com/ikala/wachen/crawler/internal/queue"
	"github.com/ikala/wachen/crawler/internal/store"
)

const (
	leaderLockKey  = 823001          // scheduler 選主用 advisory lock key
	runningTimeout = 5 * time.Minute // worker 90s fetch timeout 的安全倍數
	pendingTimeout = 10 * time.Minute
)

func main() {
	svc := bootstrap.MustInit("scheduler", "svc:scheduler")
	defer svc.Close()
	ctx, log, st, q := svc.Ctx, svc.Log, svc.Store, svc.Queue

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

	// 外層迴圈：失去 leader（PG 斷線）就退位重新競選，防止腦裂
	for ctx.Err() == nil {
		lock := acquireLeadership(ctx, log, st)
		if lock == nil {
			return // ctx done
		}
		leadLoop(ctx, log, st, q, parser, lock)
		lock.Release()
		log.Warn("leadership lost, re-electing")
	}
}

// acquireLeadership 阻塞直到成為 leader 或 ctx 結束
func acquireLeadership(ctx context.Context, log *slog.Logger, st *store.Store) *store.LeaderLock {
	for {
		lock, acquired, err := st.AcquireLeaderLock(ctx, leaderLockKey)
		if err != nil {
			log.Error("leader lock error, retrying", "err", err)
		} else if acquired {
			log.Info("became leader")
			return lock
		} else {
			log.Info("standby: leader lock held by another scheduler")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(10 * time.Second):
		}
	}
}

// leadLoop：每 tick = 心跳驗鎖 → reap 孤兒任務 → 派工。心跳失敗即返回退位。
func leadLoop(ctx context.Context, log *slog.Logger, st *store.Store, q *queue.Queue,
	parser cron.Parser, lock *store.LeaderLock) {

	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		if err := lock.Ping(ctx); err != nil {
			log.Error("leader lock heartbeat failed", "err", err)
			return // 鎖可能已被伺服器釋放，立即退位
		}
		if reaped, err := st.ReapStaleJobs(ctx, runningTimeout, pendingTimeout); err != nil {
			log.Error("reap failed", "err", err)
		} else if reaped > 0 {
			log.Warn("reaped stale jobs", "count", reaped)
		}
		scheduleDue(ctx, log, st, q, parser)
		select {
		case <-ctx.Done():
			log.Info("shutting down")
			return
		case <-tick.C:
		}
	}
}

// scheduleDue：單一 source 出錯只跳過該源（continue），不餓死其他來源
func scheduleDue(ctx context.Context, log *slog.Logger, st *store.Store, q *queue.Queue, parser cron.Parser) {
	sources, err := st.EnabledSources(ctx)
	if err != nil {
		log.Error("list sources failed", "err", err)
		return
	}
	now := time.Now()
	for _, src := range sources {
		sched, err := parser.Parse(*src.ScheduleCron)
		if err != nil {
			log.Warn("invalid cron, skip", "source", src.Name, "cron", *src.ScheduleCron)
			continue
		}
		for _, loc := range src.Locations() {
			if err := scheduleOne(ctx, st, q, src, loc, sched, now); err != nil {
				log.Error("schedule failed, skip", "source", src.Name, "location", loc, "err", err)
			}
		}
	}
}

func scheduleOne(ctx context.Context, st *store.Store, q *queue.Queue,
	src store.Source, loc string, sched cron.Schedule, now time.Time) error {

	open, err := st.HasOpenJob(ctx, src.ID, loc)
	if err != nil || open {
		return err // 上一個任務還沒跑完，不疊加
	}
	last, err := st.LastScheduledAt(ctx, src.ID, loc)
	if err != nil {
		return err
	}
	if !dueNow(sched, last, now) {
		return nil
	}
	cursor, err := st.LastSucceededCursor(ctx, src.ID, loc)
	if err != nil {
		return err
	}
	jobID, err := st.CreateJob(ctx, src.ID, loc, cursor)
	if err != nil {
		return err
	}
	// publish 失敗會留下孤兒 pending → reaper 兜底回收
	if err := q.PublishCrawlJob(ctx, src.Adapter, jobID); err != nil {
		return err
	}
	slog.Default().Info("job scheduled", "source", src.Name, "location", loc, "job_id", jobID)
	return nil
}

// dueNow：首次（無任務紀錄）立即執行，之後依 cron 由上次排程時間推算下一次
func dueNow(sched cron.Schedule, last *time.Time, now time.Time) bool {
	if last == nil {
		return true
	}
	return !sched.Next(*last).After(now)
}
