// M7 回覆留言的資料層：草稿 → 送審 → 核准/退回 → 送出，全程狀態機 + 冪等 + 稽核。
//
//	狀態機（與 ARCHITECTURE §6.1 一致）：
//	  draft ─建立─▶ pending_approval ─核准─▶ approved ─worker─▶ sending ─▶ sent
//	                     │退回                                        └─重試耗盡─▶ failed
//	                     ▼
//	                  rejected
//	  低/中風險（rule.require_approval=false）建立時直接 approved 並入列。
//	  高風險必經 pending_approval（公關/法務把關）。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrReplyNotAllowed = errors.New("source does not support reply")
	ErrReplyBadState   = errors.New("reply not in expected state")
	ErrReplyTooLong    = errors.New("reply content exceeds source limit")
	ErrCaseNotFound    = errors.New("case not found")
)

// rowQuerier：pgx.Tx 與 pgxpool.Pool 都滿足，讓 caseReplyContext 可在交易內外共用
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Reply struct {
	ID              string  `json:"id"`
	CaseID          string  `json:"case_id"`
	Content         string  `json:"content"`
	Status          string  `json:"status"`
	AuthorID        *string `json:"-"`
	ApprovedBy      *string `json:"-"`
	ExternalReplyID *string `json:"external_reply_id"`
	ReplyURL        *string `json:"reply_url"`
	Error           *string   `json:"error"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
}

// replyTarget：送出一則回覆所需的來源上下文（Reply Worker 用）
type ReplyTarget struct {
	ReplyID        string
	Content        string
	Adapter        string
	Config         json.RawMessage
	ExternalID     string // 原評論在平台上的 ID
	LocationID     string
	IdempotencyKey string // 帶給平台/callback 防重複發文
	CanReply       bool
	MaxLen         int
}

// caseReplyContext 查案件對應來源的回覆能力（建立回覆時 gate 用）
func (s *Store) caseReplyContext(ctx context.Context, q rowQuerier, caseID string) (canReply bool, maxLen int, requireApproval bool, err error) {
	var caps []byte
	err = q.QueryRow(ctx, `
		SELECT coalesce(src.capabilities, '{}'), coalesce(r.require_approval, true)
		FROM cases c
		JOIN reviews v ON v.id = c.review_id
		JOIN sources src ON src.name = v.source_name AND src.deleted_at IS NULL
		LEFT JOIN routing_rules r ON r.id = c.rule_id
		WHERE c.id = $1 AND c.deleted_at IS NULL`, caseID).Scan(&caps, &requireApproval)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, false, ErrCaseNotFound
	}
	if err != nil {
		return false, 0, false, err
	}
	var c struct {
		CanReply bool `json:"can_reply"`
		MaxLen   int  `json:"reply_max_length"`
	}
	_ = json.Unmarshal(caps, &c)
	return c.CanReply, c.MaxLen, requireApproval, nil
}

// CreateReply：建立回覆草稿。低/中風險直接 approved（回傳 enqueue=true 供 caller 入列），
// 高風險進 pending_approval。回傳 (reply, enqueue, err)。
func (s *Store) CreateReply(ctx context.Context, caseID, content, authorEmail string) (*Reply, bool, error) {
	var r Reply
	enqueue := false
	err := s.withActorTx(ctx, "user:"+authorEmail, func(tx pgx.Tx) error {
		canReply, maxLen, requireApproval, err := s.caseReplyContext(ctx, tx, caseID)
		if err != nil {
			return err
		}
		if !canReply {
			return ErrReplyNotAllowed
		}
		if maxLen > 0 && len([]rune(content)) > maxLen {
			return ErrReplyTooLong
		}
		status := "approved"
		if requireApproval {
			status = "pending_approval"
		} else {
			enqueue = true
		}
		// idempotency_key 用 DB 端 gen_random_uuid()，避免多一個 uuid 依賴
		err = tx.QueryRow(ctx, `
			INSERT INTO replies (case_id, review_id, content, status, idempotency_key, approved_at)
			SELECT c.id, c.review_id, $2, $3, gen_random_uuid()::text,
			       CASE WHEN $3 = 'approved' THEN now() END
			FROM cases c WHERE c.id = $1
			RETURNING id, case_id, content, status, external_reply_id, reply_url, error, created_by, created_at`,
			caseID, content, status).
			Scan(&r.ID, &r.CaseID, &r.Content, &r.Status, &r.ExternalReplyID, &r.ReplyURL, &r.Error, &r.CreatedBy, &r.CreatedAt)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return &r, enqueue, nil
}

// ApproveReply：pending_approval → approved（回傳 enqueue=true）
func (s *Store) ApproveReply(ctx context.Context, replyID, approverEmail string) (bool, error) {
	ok := false
	err := s.withActorTx(ctx, "user:"+approverEmail, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE replies SET status = 'approved', approved_by = NULL, approved_at = now()
			WHERE id = $1 AND status = 'pending_approval' AND deleted_at IS NULL`, replyID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrReplyBadState
		}
		ok = true
		return nil
	})
	return ok, err
}

// RejectReply：pending_approval → rejected
func (s *Store) RejectReply(ctx context.Context, replyID, approverEmail, reason string) error {
	return s.withActorTx(ctx, "user:"+approverEmail, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE replies SET status = 'rejected', error = $2
			WHERE id = $1 AND status = 'pending_approval' AND deleted_at IS NULL`, replyID, reason)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrReplyBadState
		}
		return nil
	})
}

// ClaimReplyForSend：approved → sending（Reply Worker 搶占，回傳送出上下文）
func (s *Store) ClaimReplyForSend(ctx context.Context, replyID string) (*ReplyTarget, error) {
	var t ReplyTarget
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE replies SET status = 'sending'
			WHERE id = $1 AND status = 'approved' AND deleted_at IS NULL`, replyID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrReplyBadState // 他人已搶或狀態不符
		}
		var caps []byte
		var locID *string
		err = tx.QueryRow(ctx, `
			SELECT rp.id, rp.content, rp.idempotency_key, src.adapter, src.config,
			       v.external_id, st.google_location_id, coalesce(src.capabilities, '{}')
			FROM replies rp
			JOIN reviews v ON v.id = rp.review_id
			JOIN sources src ON src.name = v.source_name AND src.deleted_at IS NULL
			LEFT JOIN stores st ON st.id = v.store_id AND st.deleted_at IS NULL
			WHERE rp.id = $1`, replyID).
			Scan(&t.ReplyID, &t.Content, &t.IdempotencyKey, &t.Adapter, &t.Config, &t.ExternalID, &locID, &caps)
		if err != nil {
			return err
		}
		if locID != nil {
			t.LocationID = *locID
		}
		var c struct {
			CanReply bool `json:"can_reply"`
			MaxLen   int  `json:"reply_max_length"`
		}
		_ = json.Unmarshal(caps, &c)
		t.CanReply, t.MaxLen = c.CanReply, c.MaxLen
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// MarkReplySent：sending → sent，記錄平台回應
func (s *Store) MarkReplySent(ctx context.Context, replyID, externalReplyID, replyURL string, platformResp json.RawMessage) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE replies SET status = 'sent',
			    external_reply_id = $2, reply_url = nullif($3, ''),
			    platform_response = $4, error = NULL
			WHERE id = $1`, replyID, externalReplyID, replyURL, platformResp)
		return err
	})
}

// MarkReplyFailed：sending → approved（重試）或 failed（耗盡）
func (s *Store) MarkReplyFailed(ctx context.Context, replyID, errMsg string, isFinal bool) error {
	status := "approved" // 退回可重送
	if isFinal {
		status = "failed"
	}
	return s.withTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE replies SET status = $2, retry_count = retry_count + 1, error = $3
			WHERE id = $1`, replyID, status, errMsg)
		return err
	})
}

// ReclaimStuckReplies：sending 卡死回收——worker 在 claim（approved→sending）之後、
// 寫回結果之前崩潰，MQ 重投遞會因狀態不符被 Ack 放過，該回覆從此無人認領。
// 超過 stuckFor 未更新的 sending 退回 approved（累計嘗試達 maxAttempts 則 failed），
// 回傳退回 approved 的 id 供 caller 重新入列。SKIP LOCKED：多 replica 對帳不互卡。
func (s *Store) ReclaimStuckReplies(ctx context.Context, stuckFor time.Duration, maxAttempts, limit int) ([]string, error) {
	var requeue []string
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			UPDATE replies SET
			    status = CASE WHEN retry_count >= $2 THEN 'failed' ELSE 'approved' END,
			    retry_count = retry_count + 1,
			    error = 'reclaimed: send interrupted (worker died mid-send)'
			WHERE id IN (
			    SELECT id FROM replies
			    WHERE status = 'sending' AND deleted_at IS NULL
			      AND updated_at < now() - $1::interval
			    ORDER BY updated_at
			    LIMIT $3
			    FOR UPDATE SKIP LOCKED)
			RETURNING id, status`, stuckFor.String(), maxAttempts, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, status string
			if err := rows.Scan(&id, &status); err != nil {
				return err
			}
			if status == "approved" {
				requeue = append(requeue, id)
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return requeue, nil
}

// StaleApprovedReplies：approved 久未被消費（API 入列失敗、publish 遺失、或剛被
// 卡死回收）→ 重新入列。與在途訊息重複無妨：ClaimReplyForSend 是冪等閘門，
// 同一則回覆只有一個消費者能搶到 approved→sending。
func (s *Store) StaleApprovedReplies(ctx context.Context, olderThan time.Duration, limit int) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id FROM replies
		WHERE status = 'approved' AND deleted_at IS NULL
		  AND updated_at < now() - $1::interval
		ORDER BY updated_at
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

// PendingApprovals：待審回覆佇列（給審核頁）
type PendingReply struct {
	ID        string   `json:"id"`
	CaseID    string   `json:"case_id"`
	Content   string   `json:"content"`
	RiskLevel string   `json:"risk_level"`
	StoreName string    `json:"store_name"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) PendingApprovals(ctx context.Context, limit int) ([]PendingReply, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT rp.id, rp.case_id, rp.content, c.risk_level,
		       coalesce(st.name, '未對映門市'), coalesce(a.summary, left(v.content, 60)), rp.created_at
		FROM replies rp
		JOIN cases c ON c.id = rp.case_id
		JOIN reviews v ON v.id = rp.review_id
		LEFT JOIN stores st ON st.id = v.store_id AND st.deleted_at IS NULL
		LEFT JOIN analysis_results a ON a.id = c.analysis_id
		WHERE rp.status = 'pending_approval' AND rp.deleted_at IS NULL
		ORDER BY array_position(ARRAY['high','medium','low'], c.risk_level), rp.created_at
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PendingReply{}
	for rows.Next() {
		var p PendingReply
		if err := rows.Scan(&p.ID, &p.CaseID, &p.Content, &p.RiskLevel,
			&p.StoreName, &p.Summary, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RepliesForCase：案件詳情用
func (s *Store) RepliesForCase(ctx context.Context, caseID string) ([]Reply, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, case_id, content, status, external_reply_id, reply_url, error, created_by, created_at
		FROM replies WHERE case_id = $1 AND deleted_at IS NULL ORDER BY created_at`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Reply{}
	for rows.Next() {
		var r Reply
		if err := rows.Scan(&r.ID, &r.CaseID, &r.Content, &r.Status,
			&r.ExternalReplyID, &r.ReplyURL, &r.Error, &r.CreatedBy, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CaseCanReply：案件詳情 gate（UI 決定是否顯示回覆框）
func (s *Store) CaseCanReply(ctx context.Context, caseID string) (bool, error) {
	canReply, _, _, err := s.caseReplyContext(ctx, s.Pool, caseID)
	if errors.Is(err, ErrCaseNotFound) {
		return false, nil
	}
	return canReply, err
}
