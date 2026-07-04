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

	"github.com/ikala/wachen/backend/internal/adapter"
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

// withTx 開交易並以服務身分（s.Actor）設定稽核操作者
func (s *Store) withTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return s.withActorTx(ctx, s.Actor, fn)
}

// withActorTx 開交易並以指定 actor 設定稽核操作者（後台操作用登入者 user:<email>）
func (s *Store) withActorTx(ctx context.Context, actor string, fn func(tx pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if actor == "" {
		actor = s.Actor
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_actor', $1, true)", actor); err != nil {
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
	Inserted bool // false = 與最新版本同內容（冪等跳過），ID 為最新版本列
}

// InsertRawReviews 整批一個交易寫入。
// 「連續去重」：只有與**最新版本**內容相同才跳過——與更早版本相同（A→B→A 回改）
// 必須成為新版本，否則回改被靜默丟棄且防回捲會把內容卡死在 B（外部審查 P1-1）。
// 以 advisory xact lock 序列化同一則評論的並發寫入。
func (s *Store) InsertRawReviews(ctx context.Context, reviews []adapter.RawReview, jobID string) ([]InsertResult, error) {
	out := make([]InsertResult, 0, len(reviews))
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		for _, r := range reviews {
			if _, err := tx.Exec(ctx,
				"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
				r.SourceName+"|"+r.ExternalID); err != nil {
				return err
			}
			var latestID, latestHash string
			err := tx.QueryRow(ctx, `
				SELECT id, content_hash FROM raw_reviews
				WHERE source_name = $1 AND external_id = $2
				ORDER BY created_at DESC, id DESC LIMIT 1`,
				r.SourceName, r.ExternalID).Scan(&latestID, &latestHash)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if err == nil && latestHash == r.ContentHash {
				out = append(out, InsertResult{ID: latestID, Inserted: false})
				continue
			}
			var id string
			if err := tx.QueryRow(ctx, `
				INSERT INTO raw_reviews
				    (source_name, external_id, payload, content_hash, source_url, location_id, fetched_at, crawl_job_id)
				VALUES ($1, $2, $3, $4, $5, nullif($6, ''), $7, nullif($8, '')::uuid)
				RETURNING id`,
				r.SourceName, r.ExternalID, r.Payload, r.ContentHash,
				r.SourceURL, r.LocationID, r.FetchedAt, jobID).Scan(&id); err != nil {
				return err
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

// EnabledWebhookSource 查啟用中的推送型來源與其驗證密鑰
func (s *Store) EnabledWebhookSource(ctx context.Context, name string) (string, bool, error) {
	var secret *string
	err := s.Pool.QueryRow(ctx, `
		SELECT config->>'webhook_secret' FROM sources
		WHERE name = $1 AND enabled AND deleted_at IS NULL`, name).Scan(&secret)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if secret == nil {
		return "", true, nil // 來源存在但沒設密鑰 → handler 會拒絕所有請求
	}
	return *secret, true, nil
}

// RawForIngest 是 ingestion 消費 review.raw 時需要的完整上下文
type RawForIngest struct {
	ID          string
	SourceName  string
	Adapter     string // 由 sources 表 join，決定用哪個 normalizer
	ExternalID  string
	Payload     []byte
	SourceURL   string
	LocationID  string
	RawCreated  time.Time
}

func (s *Store) GetRawForIngest(ctx context.Context, rawReviewID string) (*RawForIngest, error) {
	var r RawForIngest
	var sourceURL, locationID *string
	err := s.Pool.QueryRow(ctx, `
		SELECT r.id, r.source_name, s.adapter, r.external_id, r.payload,
		       r.source_url, r.location_id, r.created_at
		FROM raw_reviews r
		JOIN sources s ON s.name = r.source_name
		WHERE r.id = $1`, rawReviewID).
		Scan(&r.ID, &r.SourceName, &r.Adapter, &r.ExternalID, &r.Payload,
			&sourceURL, &locationID, &r.RawCreated)
	if err != nil {
		return nil, err
	}
	if sourceURL != nil {
		r.SourceURL = *sourceURL
	}
	if locationID != nil {
		r.LocationID = *locationID
	}
	return &r, nil
}

// UpsertReviewParams 是正規化後要落 reviews 表的欄位
type UpsertReviewParams struct {
	RawReviewID string
	SourceName  string
	ExternalID  string
	AuthorName  string
	Rating      *float64
	Content     string
	PostedAt    *time.Time
	SourceURL   string
	LocationID  string // 由此解析 store_id（stores.google_location_id）
}

// UpsertOutcome 決定要不要（重）發 review.created 與是否重新分析：
//   Applied    ：首見或正規化內容有變 → status='new'、發事件
//   PointerOnly：raw 版本前進但顧客內容未變（如商家回覆、updateTime 抖動）
//                → 只更新指標欄位，status 不動、不發事件（否則 M7 回覆會自我觸發重分析）
//   Replay     ：同 raw 重放（publish 失敗後重試）→ 補發事件，下游冪等
//   Stale      ：嚴格過時版本（事件亂序）→ 不動、不發
//   Deleted    ：目標已軟刪除 → 不復活、不發（否則殭屍列進 M4）
type UpsertOutcome int

const (
	UpsertApplied UpsertOutcome = iota
	UpsertPointerOnly
	UpsertReplay
	UpsertStale
	UpsertDeleted
)

func (o UpsertOutcome) String() string {
	return [...]string{"applied", "pointer_only", "replay", "stale", "deleted"}[o]
}

// UpsertReview：一則評論（source, external_id）只有一列。
// 顯式兩步（SELECT FOR UPDATE → 比對 → UPDATE）而非 ON CONFLICT 巧技——
// 需要區分五種結果，明確勝於聰明。audit trigger 自動留痕舊值。
func (s *Store) UpsertReview(ctx context.Context, p UpsertReviewParams) (string, UpsertOutcome, error) {
	var id string
	var outcome UpsertOutcome
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var cur struct {
			rawID      string
			content    string
			rating     *float64
			deletedAt  *time.Time
			rawCreated time.Time
		}
		scanErr := tx.QueryRow(ctx, `
			SELECT v.id, v.raw_review_id, v.content, v.rating, v.deleted_at, r.created_at
			FROM reviews v JOIN raw_reviews r ON r.id = v.raw_review_id
			WHERE v.source_name = $1 AND v.external_id = $2
			FOR UPDATE OF v`,
			p.SourceName, p.ExternalID).
			Scan(&id, &cur.rawID, &cur.content, &cur.rating, &cur.deletedAt, &cur.rawCreated)

		if errors.Is(scanErr, pgx.ErrNoRows) {
			outcome = UpsertApplied
			return tx.QueryRow(ctx, `
				INSERT INTO reviews
				    (raw_review_id, source_name, external_id, author_name, rating,
				     content, posted_at, source_url, store_id, status)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
				        (SELECT id FROM stores WHERE google_location_id = nullif($9, '') AND deleted_at IS NULL),
				        'new')
				RETURNING id`,
				p.RawReviewID, p.SourceName, p.ExternalID, p.AuthorName, p.Rating,
				p.Content, p.PostedAt, p.SourceURL, p.LocationID).Scan(&id)
		}
		if scanErr != nil {
			return scanErr
		}
		if cur.deletedAt != nil {
			outcome = UpsertDeleted
			return nil
		}
		if cur.rawID == p.RawReviewID {
			outcome = UpsertReplay
			return nil
		}
		var newCreated time.Time
		if err := tx.QueryRow(ctx,
			`SELECT created_at FROM raw_reviews WHERE id = $1`, p.RawReviewID).Scan(&newCreated); err != nil {
			return err
		}
		// 嚴格較舊才算 Stale；平手（同批交易時戳）以到達順序為準（外部審查 P2-8）
		if newCreated.Before(cur.rawCreated) {
			outcome = UpsertStale
			return nil
		}
		contentChanged := cur.content != p.Content || !floatPtrEq(cur.rating, p.Rating)
		if contentChanged {
			outcome = UpsertApplied
		} else {
			outcome = UpsertPointerOnly
		}
		_, err := tx.Exec(ctx, `
			UPDATE reviews SET
			    raw_review_id = $2,
			    author_name   = $3,
			    rating        = $4,
			    content       = $5,
			    posted_at     = $6,
			    source_url    = $7,
			    store_id      = coalesce((SELECT id FROM stores WHERE google_location_id = nullif($8, '') AND deleted_at IS NULL), store_id),
			    status        = CASE WHEN $9 THEN 'new' ELSE status END
			WHERE id = $1`,
			id, p.RawReviewID, p.AuthorName, p.Rating, p.Content, p.PostedAt,
			p.SourceURL, p.LocationID, contentChanged)
		return err
	})
	return id, outcome, err
}

func floatPtrEq(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// FindUnreflectedRaws 找「最新版本尚未反映到 reviews」的 raw id——
// ingestion 對帳掃描第一條腿：死信、漏發的 review.raw、亂序殘留。
// 排除 ingest_quarantine（normalize 失敗的毒藥 raw，否則每輪重撿無限迴圈）。
// olderThan 避開仍在佇列中的 in-flight 事件。
func (s *Store) FindUnreflectedRaws(ctx context.Context, olderThan time.Duration, limit int) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT latest.id FROM (
		    SELECT DISTINCT ON (r.source_name, r.external_id) r.id, v.id AS reflected
		    FROM raw_reviews r
		    JOIN sources s ON s.name = r.source_name AND s.deleted_at IS NULL
		    LEFT JOIN reviews v
		      ON v.source_name = r.source_name
		     AND v.external_id = r.external_id
		     AND v.raw_review_id = r.id
		    WHERE r.created_at < now() - $1::interval
		    ORDER BY r.source_name, r.external_id, r.created_at DESC, r.id DESC
		) latest
		LEFT JOIN ingest_quarantine q ON q.raw_review_id = latest.id
		WHERE latest.reflected IS NULL AND q.raw_review_id IS NULL
		LIMIT $2`, olderThan.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// FindStaleNewReviews 對帳掃描第二條腿：status='new' 卻遲遲沒被 M4 消化的 reviews，
// 重發 review.created——堵「upsert 已 commit 但 publish 重試耗盡」的黑洞
// （該情況下 raw 與 reviews 完全一致，第一條腿看不見；外部審查 P1-2）。
func (s *Store) FindStaleNewReviews(ctx context.Context, olderThan time.Duration, limit int) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id FROM reviews
		WHERE status = 'new' AND deleted_at IS NULL
		  AND updated_at < now() - $1::interval
		LIMIT $2`, olderThan.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// QuarantineRaw 隔離 normalize 失敗的 raw；人工修復後 DELETE 該列即重入掃描
func (s *Store) QuarantineRaw(ctx context.Context, rawReviewID, reason string) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO ingest_quarantine (raw_review_id, reason)
			VALUES ($1, $2) ON CONFLICT (raw_review_id) DO NOTHING`, rawReviewID, reason)
		return err
	})
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
