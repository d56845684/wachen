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

// ListCases：收件匣。risk/status 空字串 = 不過濾；排序：逾期優先 → SLA 逼近優先
func (s *Store) ListCases(ctx context.Context, risk, status string, limit int) ([]CaseSummary, error) {
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
		  AND v.source_name NOT LIKE 'test_%'
		ORDER BY (c.sla_due_at < now() AND c.status IN ('open','in_progress')) DESC,
		         c.sla_due_at ASC
		LIMIT $3`, risk, status, limit)
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
