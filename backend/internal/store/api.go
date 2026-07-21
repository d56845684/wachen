// M6 後台 API 的資料層：登入驗證、案件列表/詳情、狀態變更。
// 稽核：API 寫入的 actor 是「登入使用者」而非服務身分（user:<email>）。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type AuthedUser struct {
	ID          string
	Email       string
	DisplayName string
	Role        string
}

// EnsureUser：以環境變數提供的帳密建立/更新使用者（API 啟動時呼叫）。
// 密碼永不進版控——bcrypt 由 pgcrypto crypt(+gen_salt('bf')) 產生。冪等。
func (s *Store) EnsureUser(ctx context.Context, email, password, role, displayName string) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO users (email, display_name, role, password_hash, is_active, created_by, updated_by)
			VALUES ($1, $3, $4, crypt($2, gen_salt('bf')), true, current_actor(), current_actor())
			ON CONFLICT (email) DO UPDATE SET
			    display_name = $3, role = $4,
			    password_hash = crypt($2, gen_salt('bf')),
			    is_active = true`, email, password, displayName, role)
		return err
	})
}

// EnsureAdmin：EnsureUser 的管理員捷徑（向後相容）。
func (s *Store) EnsureAdmin(ctx context.Context, email, password string) error {
	return s.EnsureUser(ctx, email, password, "admin", "系統管理員")
}

// AuthUser 帳密驗證：bcrypt 比對走 pgcrypto crypt()，常數時間由 bcrypt 本身保證
func (s *Store) AuthUser(ctx context.Context, email, password string) (*AuthedUser, error) {
	var u AuthedUser
	err := s.Pool.QueryRow(ctx, `
		SELECT id, email, display_name, role FROM users
		WHERE email = $1 AND is_active AND deleted_at IS NULL
		  AND password_hash IS NOT NULL
		  AND password_hash = crypt($2, password_hash)`, email, password).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // 帳號不存在與密碼錯誤不可區分
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

type CaseSummary struct {
	ID            string     `json:"id"`
	RiskLevel     string     `json:"risk_level"`
	Status        string     `json:"status"`
	SLADueAt      time.Time  `json:"sla_due_at"`
	SLAReminded   bool       `json:"sla_reminded"`
	ReopenedCount int        `json:"reopened_count"`
	CreatedAt     time.Time  `json:"created_at"`
	StoreName     string     `json:"store_name"`
	SourceName    string     `json:"source_name"`
	SourceURL     string     `json:"source_url"`
	Rating        *float64   `json:"rating"`
	Summary       string     `json:"summary"`
	Sentiment     string     `json:"sentiment"`
	Categories    []string   `json:"categories"`
	PostedAt      *time.Time `json:"posted_at"`
}

// CaseFilter：收件匣篩選條件，各欄位空字串 = 不過濾
type CaseFilter struct {
	Risk   string
	Status string
	Store  string // stores.google_location_id（未對映門市用特殊值 "__none__"）
	Source string // reviews.source_name
	Rating string // 星等精確篩選（"1"~"5"，空=全部）；僅對有星等的來源（Google）有意義
	Sort   string // sla（預設）/ newest / oldest（依評論張貼時間）
	Limit  int
}

// orderClause：白名單映射，避免把使用者輸入拼進 SQL。
// 評論時間排序把 NULL（無 posted_at 的來源）放最後。
var orderClause = map[string]string{
	"sla":    "(c.sla_due_at < now() AND c.status IN ('open','in_progress')) DESC, c.sla_due_at ASC",
	"newest": "v.posted_at DESC NULLS LAST, c.created_at DESC",
	"oldest": "v.posted_at ASC NULLS LAST, c.created_at ASC",
}

// validRatings：星等篩選白名單（Google 評論為整數星等 1-5）。
var validRatings = map[string]bool{"1": true, "2": true, "3": true, "4": true, "5": true}

// normalizeRating：對齊 orderClause 的降級策略——非白名單值（空字串、非數字、
// 注入嘗試、無此星等如 4.5/6）一律當「全部」，不進查詢。避免 v.rating = $5::numeric
// 對惡意輸入丟 cast error 變 500（其餘篩選對壞值都是回空，星等須一致）。
func normalizeRating(r string) string {
	if validRatings[r] {
		return r
	}
	return ""
}

// ListCases：收件匣。預設排序：逾期優先 → SLA 逼近優先
func (s *Store) ListCases(ctx context.Context, f CaseFilter) ([]CaseSummary, error) {
	order, ok := orderClause[f.Sort]
	if !ok {
		order = orderClause["sla"]
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.risk_level, c.status, c.sla_due_at,
		       c.sla_reminded_at IS NOT NULL, c.reopened_count, c.created_at,
		       coalesce(st.name, ''), v.source_name, v.source_url, v.rating,
		       coalesce(a.summary, left(v.content, 60)),
		       coalesce(a.sentiment, ''), coalesce(a.categories, '{}'), v.posted_at
		FROM cases c
		JOIN reviews v ON v.id = c.review_id
		LEFT JOIN stores st ON st.id = v.store_id AND st.deleted_at IS NULL
		LEFT JOIN analysis_results a ON a.id = c.analysis_id
		WHERE c.deleted_at IS NULL
		  AND ($1 = '' OR c.risk_level = $1)
		  AND ($2 = '' OR c.status = $2)
		  AND ($3 = '' OR st.google_location_id = $3 OR ($3 = '__none__' AND v.store_id IS NULL))
		  AND ($4 = '' OR v.source_name = $4)
		  AND ($5 = '' OR v.rating = $5::numeric)
		  AND v.source_name NOT LIKE 'test_%'
		ORDER BY `+order+`
		LIMIT $6`, f.Risk, f.Status, f.Store, f.Source, normalizeRating(f.Rating), f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CaseSummary
	for rows.Next() {
		var cs CaseSummary
		if err := rows.Scan(&cs.ID, &cs.RiskLevel, &cs.Status, &cs.SLADueAt,
			&cs.SLAReminded, &cs.ReopenedCount, &cs.CreatedAt,
			&cs.StoreName, &cs.SourceName, &cs.SourceURL, &cs.Rating,
			&cs.Summary, &cs.Sentiment, &cs.Categories, &cs.PostedAt); err != nil {
			return nil, err
		}
		out = append(out, cs)
	}
	return out, rows.Err()
}

type Facet struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// CaseFacets：目前有案件的門市與來源清單（供收件匣下拉選單，附案件數）
func (s *Store) CaseFacets(ctx context.Context) (stores, sources []Facet, err error) {
	stores, err = s.queryFacets(ctx, `
		SELECT coalesce(st.google_location_id, '__none__'),
		       coalesce(st.name, '未對映門市'), count(*)
		FROM cases c
		JOIN reviews v ON v.id = c.review_id
		LEFT JOIN stores st ON st.id = v.store_id AND st.deleted_at IS NULL
		WHERE c.deleted_at IS NULL AND v.source_name NOT LIKE 'test_%'
		GROUP BY 1, 2 ORDER BY 3 DESC`)
	if err != nil {
		return nil, nil, err
	}
	sources, err = s.queryFacets(ctx, `
		SELECT v.source_name, v.source_name, count(*)
		FROM cases c
		JOIN reviews v ON v.id = c.review_id
		WHERE c.deleted_at IS NULL AND v.source_name NOT LIKE 'test_%'
		GROUP BY 1 ORDER BY 3 DESC`)
	return stores, sources, err
}

func (s *Store) queryFacets(ctx context.Context, sql string) ([]Facet, error) {
	rows, err := s.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Facet{}
	for rows.Next() {
		var f Facet
		if err := rows.Scan(&f.Value, &f.Label, &f.Count); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

type PipelineStats struct {
	Funnel struct {
		RawReviews       int `json:"raw_reviews"`
		Reviews          int `json:"reviews"`
		AwaitingAnalysis int `json:"awaiting_analysis"` // reviews.status = 'new'
		Analyzed         int `json:"analyzed"`          // 有現行分析
		AwaitingRouting  int `json:"awaiting_routing"`  // 已分析但未建案
		Cased            int `json:"cased"`             // 有案件
	} `json:"funnel"`
	AI struct {
		Models          []string `json:"models"`     // 現行分析用過的模型（heuristic / gemini）
		TotalAnalyses   int      `json:"total_analyses"`
		AvgLatencyMs    int      `json:"avg_latency_ms"`
		MaxLatencyMs    int      `json:"max_latency_ms"`
		QuarantineCount int      `json:"quarantine_count"`
		Last5Min        int      `json:"last_5min"`
		LastHour        int      `json:"last_hour"`
		FallbackCount   int      `json:"fallback_count"` // Gemini 失敗降級 heuristic 的筆數
	} `json:"ai"`
	Risk      []Facet          `json:"risk"`
	Sentiment []Facet          `json:"sentiment"`
	Recent    []RecentAnalysis `json:"recent"`
}

type RecentAnalysis struct {
	ReviewID   string    `json:"review_id"`
	StoreName  string    `json:"store_name"`
	SourceName string    `json:"source_name"`
	RiskLevel  string    `json:"risk_level"`
	Sentiment  string    `json:"sentiment"`
	ModelName  string    `json:"model_name"`
	LatencyMs  *int      `json:"latency_ms"`
	Summary    string    `json:"summary"`
	CreatedAt  time.Time `json:"created_at"`
	Fallback   bool      `json:"fallback"`
}

// PipelineStats：AI 處理進度分頁的資料（排除 test_* 殘留）
func (s *Store) GetPipelineStats(ctx context.Context) (*PipelineStats, error) {
	var p PipelineStats
	const notTest = "v.source_name NOT LIKE 'test_%'"

	// 漏斗
	if err := s.Pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM raw_reviews r WHERE r.source_name NOT LIKE 'test_%'),
		  (SELECT count(*) FROM reviews v WHERE `+notTest+` AND v.deleted_at IS NULL),
		  (SELECT count(*) FROM reviews v WHERE `+notTest+` AND v.deleted_at IS NULL AND v.status = 'new'),
		  (SELECT count(*) FROM reviews v WHERE v.deleted_at IS NULL AND `+notTest+`
		     AND EXISTS (SELECT 1 FROM analysis_results a WHERE a.review_id = v.id AND a.is_current)),
		  (SELECT count(*) FROM reviews v WHERE v.deleted_at IS NULL AND `+notTest+`
		     AND EXISTS (SELECT 1 FROM analysis_results a WHERE a.review_id = v.id AND a.is_current)
		     AND NOT EXISTS (SELECT 1 FROM cases c WHERE c.review_id = v.id AND c.deleted_at IS NULL)),
		  (SELECT count(*) FROM cases c JOIN reviews v ON v.id = c.review_id
		     WHERE c.deleted_at IS NULL AND `+notTest+`)`).
		Scan(&p.Funnel.RawReviews, &p.Funnel.Reviews, &p.Funnel.AwaitingAnalysis,
			&p.Funnel.Analyzed, &p.Funnel.AwaitingRouting, &p.Funnel.Cased); err != nil {
		return nil, err
	}

	// AI 統計（現行分析）
	if err := s.Pool.QueryRow(ctx, `
		SELECT count(*),
		       coalesce(round(avg(latency_ms)), 0), coalesce(max(latency_ms), 0),
		       count(*) FILTER (WHERE created_at > now() - interval '5 minutes'),
		       count(*) FILTER (WHERE created_at > now() - interval '1 hour'),
		       count(*) FILTER (WHERE raw_response ? 'fallback_from')
		FROM analysis_results a
		WHERE a.is_current AND a.deleted_at IS NULL
		  AND EXISTS (SELECT 1 FROM reviews v WHERE v.id = a.review_id AND `+notTest+`)`).
		Scan(&p.AI.TotalAnalyses, &p.AI.AvgLatencyMs, &p.AI.MaxLatencyMs,
			&p.AI.Last5Min, &p.AI.LastHour, &p.AI.FallbackCount); err != nil {
		return nil, err
	}
	if err := s.Pool.QueryRow(ctx,
		`SELECT count(*) FROM ingest_quarantine`).Scan(&p.AI.QuarantineCount); err != nil {
		return nil, err
	}
	models, err := s.scanStrings(ctx, `
		SELECT DISTINCT model_name FROM analysis_results
		WHERE is_current AND deleted_at IS NULL ORDER BY model_name`)
	if err != nil {
		return nil, err
	}
	p.AI.Models = models

	// 風險與情緒分布（現行分析）
	if p.Risk, err = s.queryFacets(ctx, `
		SELECT a.risk_level, a.risk_level, count(*)
		FROM analysis_results a JOIN reviews v ON v.id = a.review_id
		WHERE a.is_current AND a.deleted_at IS NULL AND `+notTest+`
		GROUP BY 1 ORDER BY array_position(ARRAY['high','medium','low'], a.risk_level)`); err != nil {
		return nil, err
	}
	if p.Sentiment, err = s.queryFacets(ctx, `
		SELECT a.sentiment, a.sentiment, count(*)
		FROM analysis_results a JOIN reviews v ON v.id = a.review_id
		WHERE a.is_current AND a.deleted_at IS NULL AND `+notTest+` AND a.sentiment IS NOT NULL
		GROUP BY 1 ORDER BY 3 DESC`); err != nil {
		return nil, err
	}

	// 最近 15 筆分析
	rows, err := s.Pool.Query(ctx, `
		SELECT v.id, coalesce(st.name, ''), v.source_name,
		       a.risk_level, coalesce(a.sentiment, ''), a.model_name, a.latency_ms,
		       coalesce(a.summary, ''), a.created_at, (a.raw_response ? 'fallback_from')
		FROM analysis_results a
		JOIN reviews v ON v.id = a.review_id
		LEFT JOIN stores st ON st.id = v.store_id AND st.deleted_at IS NULL
		WHERE a.is_current AND a.deleted_at IS NULL AND `+notTest+`
		ORDER BY a.created_at DESC LIMIT 15`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	p.Recent = []RecentAnalysis{}
	for rows.Next() {
		var r RecentAnalysis
		if err := rows.Scan(&r.ReviewID, &r.StoreName, &r.SourceName, &r.RiskLevel,
			&r.Sentiment, &r.ModelName, &r.LatencyMs, &r.Summary, &r.CreatedAt, &r.Fallback); err != nil {
			return nil, err
		}
		p.Recent = append(p.Recent, r)
	}
	return &p, rows.Err()
}

func (s *Store) scanStrings(ctx context.Context, sql string) ([]string, error) {
	rows, err := s.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type CaseDetail struct {
	CaseSummary
	ReviewContent string          `json:"review_content"`
	AuthorName    string          `json:"author_name"`
	Keywords      []string        `json:"keywords"`
	RiskReasons   []string        `json:"risk_reasons"`
	SentimentSc   *float64        `json:"sentiment_score"`
	ModelName     string          `json:"model_name"`
	PromptVersion string          `json:"prompt_version"`
	Assignments   []string        `json:"assignments"`
	Notifications json.RawMessage `json:"notifications"` // [{channel,recipient,subject,status,sent_at}]
	CanReply      bool            `json:"can_reply"`
	Replies       []Reply         `json:"replies"`
}

func (s *Store) GetCaseDetail(ctx context.Context, caseID string) (*CaseDetail, error) {
	var d CaseDetail
	var author *string
	err := s.Pool.QueryRow(ctx, `
		SELECT c.id, c.risk_level, c.status, c.sla_due_at,
		       c.sla_reminded_at IS NOT NULL, c.reopened_count, c.created_at,
		       coalesce(st.name, ''), v.source_name, v.source_url, v.rating,
		       coalesce(a.summary, ''), coalesce(a.sentiment, ''),
		       coalesce(a.categories, '{}'), v.posted_at,
		       v.content, v.author_name,
		       coalesce(a.keywords, '{}'), coalesce(a.risk_reasons, '{}'),
		       a.sentiment_score, coalesce(a.model_name, ''), coalesce(a.prompt_version, ''),
		       coalesce((SELECT array_agg(DISTINCT assignee_role) FROM case_assignments
		                 WHERE case_id = c.id AND deleted_at IS NULL), '{}'),
		       coalesce((SELECT json_agg(json_build_object(
		                   'channel', n.channel, 'recipient', n.recipient,
		                   'subject', n.subject, 'status', n.status, 'sent_at', n.sent_at)
		                   ORDER BY n.created_at DESC)
		                 FROM notifications n WHERE n.case_id = c.id), '[]')
		FROM cases c
		JOIN reviews v ON v.id = c.review_id
		LEFT JOIN stores st ON st.id = v.store_id AND st.deleted_at IS NULL
		LEFT JOIN analysis_results a ON a.id = c.analysis_id
		WHERE c.id = $1 AND c.deleted_at IS NULL`, caseID).
		Scan(&d.ID, &d.RiskLevel, &d.Status, &d.SLADueAt,
			&d.SLAReminded, &d.ReopenedCount, &d.CreatedAt,
			&d.StoreName, &d.SourceName, &d.SourceURL, &d.Rating,
			&d.Summary, &d.Sentiment, &d.Categories, &d.PostedAt,
			&d.ReviewContent, &author,
			&d.Keywords, &d.RiskReasons,
			&d.SentimentSc, &d.ModelName, &d.PromptVersion,
			&d.Assignments, &d.Notifications)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if author != nil {
		d.AuthorName = *author
	}
	if d.CanReply, _, _, err = s.caseReplyContext(ctx, s.Pool, caseID); err != nil {
		return nil, err
	}
	if d.Replies, err = s.RepliesForCase(ctx, caseID); err != nil {
		return nil, err
	}
	return &d, nil
}

// 合法的人工狀態轉換（reopen 由 Routing 依新分析觸發，不開放手動）
var validTransitions = map[string]map[string]bool{
	"open":        {"in_progress": true, "resolved": true, "ignored": false},
	"in_progress": {"resolved": true, "open": true},
	"resolved":    {"closed": true, "in_progress": true},
	"closed":      {},
}

var ErrInvalidTransition = errors.New("invalid status transition")

// UpdateCaseStatus：以「登入使用者」為稽核 actor（非服務身分）
func (s *Store) UpdateCaseStatus(ctx context.Context, caseID, newStatus, actor string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_actor', $1, true)", actor); err != nil {
		return err
	}
	var cur string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM cases WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, caseID).
		Scan(&cur); err != nil {
		return err
	}
	if !validTransitions[cur][newStatus] {
		return ErrInvalidTransition
	}
	set := "status = $2"
	if newStatus == "resolved" {
		set += ", responded_at = coalesce(responded_at, now())"
	}
	if _, err := tx.Exec(ctx,
		`UPDATE cases SET `+set+` WHERE id = $1`, caseID, newStatus); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
