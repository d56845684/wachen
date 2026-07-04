// Package store 封裝 PostgreSQL 存取。
// 稽核約定：所有寫入都在交易內先 set_config('app.current_actor', ...)，
// 讓 audit trigger 記錄操作者。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikala/wachen/crawler/internal/adapter"
)

type Store struct {
	Pool  *pgxpool.Pool
	Actor string // 例如 "svc:crawler-worker:worker-1"
}

func New(ctx context.Context, dsn, actor string) (*Store, error) {
	var pool *pgxpool.Pool
	var err error
	// 服務啟動可能早於 DB ready，重試 30 次
	for i := 0; i < 30; i++ {
		pool, err = pgxpool.New(ctx, dsn)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return &Store{Pool: pool, Actor: actor}, nil
			}
			pool.Close()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("connect db: %w", err)
}

// withTx 開交易並設定稽核操作者
func (s *Store) withTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_actor', $1, true)", s.Actor); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type Source struct {
	ID           string
	Name         string
	Adapter      string
	Config       json.RawMessage
	ScheduleCron *string
	CreatedAt    time.Time
}

// Locations 解析 config 的 location_ids；無 location 概念的來源回傳單一空字串
// （仍會排一個 job，location_id 為 NULL）
func (src Source) Locations() []string {
	var cfg struct {
		LocationIDs []string `json:"location_ids"`
	}
	_ = json.Unmarshal(src.Config, &cfg)
	if len(cfg.LocationIDs) == 0 {
		return []string{""}
	}
	return cfg.LocationIDs
}

func (s *Store) EnabledSources(ctx context.Context) ([]Source, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, name, adapter, config, schedule_cron, created_at
		FROM sources
		WHERE enabled AND deleted_at IS NULL AND schedule_cron IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		var src Source
		if err := rows.Scan(&src.ID, &src.Name, &src.Adapter, &src.Config, &src.ScheduleCron, &src.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// LastScheduledAt 回傳該 source+location 最近一次任務的排程時間（nil = 從未排程）
func (s *Store) LastScheduledAt(ctx context.Context, sourceID, locationID string) (*time.Time, error) {
	var t *time.Time
	err := s.Pool.QueryRow(ctx, `
		SELECT max(scheduled_at) FROM crawl_jobs
		WHERE source_id = $1 AND location_id IS NOT DISTINCT FROM nullif($2, '')`,
		sourceID, locationID).Scan(&t)
	return t, err
}

func (s *Store) HasOpenJob(ctx context.Context, sourceID, locationID string) (bool, error) {
	var exists bool
	err := s.Pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM crawl_jobs
		               WHERE source_id = $1
		                 AND location_id IS NOT DISTINCT FROM nullif($2, '')
		                 AND status IN ('pending', 'running'))`, sourceID, locationID).Scan(&exists)
	return exists, err
}

// LastSucceededCursor 取該 source+location 上次成功任務的 cursor，作為增量起點
func (s *Store) LastSucceededCursor(ctx context.Context, sourceID, locationID string) (adapter.Cursor, error) {
	var raw []byte
	err := s.Pool.QueryRow(ctx, `
		SELECT cursor_state FROM crawl_jobs
		WHERE source_id = $1
		  AND location_id IS NOT DISTINCT FROM nullif($2, '')
		  AND status = 'succeeded' AND cursor_state IS NOT NULL
		ORDER BY finished_at DESC LIMIT 1`, sourceID, locationID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c adapter.Cursor
	return c, json.Unmarshal(raw, &c)
}

func (s *Store) CreateJob(ctx context.Context, sourceID, locationID string, cursor adapter.Cursor) (string, error) {
	var id string
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		cur, err := json.Marshal(cursor)
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO crawl_jobs (source_id, location_id, cursor_state)
			VALUES ($1, nullif($2, ''), $3)
			RETURNING id`, sourceID, locationID, cur).Scan(&id)
	})
	return id, err
}

func (s *Store) GetJob(ctx context.Context, jobID string) (*adapter.CrawlJob, error) {
	var job adapter.CrawlJob
	var cursorRaw []byte
	var locationID, placeID *string
	err := s.Pool.QueryRow(ctx, `
		SELECT j.id, j.source_id, s.name, s.adapter, s.config, j.cursor_state,
		       j.location_id, st.google_place_id
		FROM crawl_jobs j
		JOIN sources s ON s.id = j.source_id
		LEFT JOIN stores st ON st.google_location_id = j.location_id AND st.deleted_at IS NULL
		WHERE j.id = $1`, jobID).
		Scan(&job.ID, &job.SourceID, &job.SourceName, &job.Adapter, &job.Config,
			&cursorRaw, &locationID, &placeID)
	if err != nil {
		return nil, err
	}
	if locationID != nil {
		job.LocationID = *locationID
	}
	if placeID != nil {
		job.PlaceID = *placeID
	}
	if len(cursorRaw) > 0 {
		_ = json.Unmarshal(cursorRaw, &job.Cursor)
	}
	return &job, nil
}

// ClaimJob 搶占任務（分散式 worker 靠這裡避免重複執行）
func (s *Store) ClaimJob(ctx context.Context, jobID, workerID string) (bool, error) {
	claimed := false
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE crawl_jobs
			SET status = 'running', worker_id = $2, started_at = now(), error = NULL
			WHERE id = $1 AND status IN ('pending', 'failed')`, jobID, workerID)
		claimed = tag.RowsAffected() == 1
		return err
	})
	return claimed, err
}

type JobStats struct {
	Fetched    int  `json:"fetched"`
	Inserted   int  `json:"inserted"`
	Duplicates int  `json:"duplicates"`
	PageCapHit bool `json:"page_cap_hit,omitempty"` // 3A：首次同步截斷不靜默
}

func (s *Store) FinishJob(ctx context.Context, jobID, status string, cursor adapter.Cursor, stats JobStats, errMsg string) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		cur, err := json.Marshal(cursor)
		if err != nil {
			return err
		}
		st, _ := json.Marshal(stats)
		var e *string
		if errMsg != "" {
			e = &errMsg
		}
		_, err = tx.Exec(ctx, `
			UPDATE crawl_jobs
			SET status = $2, cursor_state = $3, stats = $4, error = $5, finished_at = now()
			WHERE id = $1`, jobID, status, cur, st, e)
		return err
	})
}

// ReapStaleJobs 回收孤兒任務（1A + 外部聲音的 pending 補充）：
//   running 超時 = worker 死在半路；pending 超時 = CreateJob 後 publish 失敗。
// 兩者都會讓 HasOpenJob 永遠 true、來源靜默停擺——改成 failed 讓 cron 自然重排。
func (s *Store) ReapStaleJobs(ctx context.Context, runningTimeout, pendingTimeout time.Duration) (int64, error) {
	var reaped int64
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE crawl_jobs
			SET status = 'failed', finished_at = now(),
			    error = 'reaped: stale ' || status || ' job'
			WHERE (status = 'running' AND started_at < now() - $1::interval)
			   OR (status = 'pending' AND created_at < now() - $2::interval)`,
			runningTimeout.String(), pendingTimeout.String())
		reaped = tag.RowsAffected()
		return err
	})
	return reaped, err
}

type InsertResult struct {
	ID       string
	Inserted bool // false = 同版本已存在（冪等跳過），ID 仍為既有列
}

// InsertRawReviews 整批一個交易寫入（效能修正）。
// 版本化冪等（T1-A）：同 (source, external_id, content_hash) 跳過並回查既有 id——
// 內容變了（編輯）就是新列。不用 ON CONFLICT DO UPDATE：raw_reviews 有防篡改 trigger。
func (s *Store) InsertRawReviews(ctx context.Context, reviews []adapter.RawReview, jobID string) ([]InsertResult, error) {
	out := make([]InsertResult, 0, len(reviews))
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		for _, r := range reviews {
			var id string
			scanErr := tx.QueryRow(ctx, `
				INSERT INTO raw_reviews
				    (source_name, external_id, payload, content_hash, source_url, location_id, fetched_at, crawl_job_id)
				VALUES ($1, $2, $3, $4, $5, nullif($6, ''), $7, $8)
				ON CONFLICT (source_name, external_id, content_hash) DO NOTHING
				RETURNING id`,
				r.SourceName, r.ExternalID, r.Payload, r.ContentHash,
				r.SourceURL, r.LocationID, r.FetchedAt, jobID).Scan(&id)
			if errors.Is(scanErr, pgx.ErrNoRows) {
				// 既有版本：回查 id，讓 caller 仍可補發事件（2A）
				if err := tx.QueryRow(ctx, `
					SELECT id FROM raw_reviews
					WHERE source_name = $1 AND external_id = $2 AND content_hash = $3`,
					r.SourceName, r.ExternalID, r.ContentHash).Scan(&id); err != nil {
					return err
				}
				out = append(out, InsertResult{ID: id, Inserted: false})
				continue
			}
			if scanErr != nil {
				return scanErr
			}
			out = append(out, InsertResult{ID: id, Inserted: true})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// LeaderLock 是帶心跳的 advisory lock：PG 斷線會使鎖被伺服器釋放，
// 舊 leader 必須能發現並退位，否則腦裂（雙 leader 重複派工燒 API 配額）。
type LeaderLock struct {
	conn *pgxpool.Conn
	key  int64
}

// Ping 驗證鎖連線仍活著；error = 鎖已丟，caller 必須退位
func (l *LeaderLock) Ping(ctx context.Context) error {
	var ok bool
	return l.conn.QueryRow(ctx, "SELECT true").Scan(&ok)
}

func (l *LeaderLock) Release() {
	_, _ = l.conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", l.key)
	l.conn.Release()
}

// AcquireLeaderLock 以 PG advisory lock 做 scheduler 選主
func (s *Store) AcquireLeaderLock(ctx context.Context, key int64) (*LeaderLock, bool, error) {
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&got); err != nil {
		conn.Release()
		return nil, false, err
	}
	if !got {
		conn.Release()
		return nil, false, nil
	}
	return &LeaderLock{conn: conn, key: key}, true, nil
}
