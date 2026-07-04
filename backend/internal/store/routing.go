// M5 Routing 的資料層：案件路由交易、analyzed-未建案對帳、SLA 提醒、通知佇列。
//
//	案件狀態 × 新分析 的決策矩陣（reopen 語意，cases.review_id UNIQUE）：
//
//	                 │ 無案件   │ open/in_progress      │ resolved/closed
//	  ───────────────┼──────────┼───────────────────────┼─────────────────
//	  同 analysis_id │    —     │ Replay（補發事件）     │ Replay
//	  新分析,風險 ↑  │ Created  │ Escalated（換規則/SLA）│ Reopened
//	  新分析,風險 =↓ │ Created  │ Acknowledged（收指標） │ Reopened
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type RoutingRule struct {
	ID              string
	RiskLevel       string
	AssigneeRoles   []string
	SLAHours        int
	RequireApproval bool
}

// CurrentAnalysisForRouting 依 review_id 重讀現行分析（事件僅是提示，不信 payload）
type RoutableAnalysis struct {
	AnalysisID string
	ReviewID   string
	RiskLevel  string
	Summary    string
	SourceURL  string
	StoreName  string // 可能為空（webhook 無對映）
}

func (s *Store) CurrentAnalysisForRouting(ctx context.Context, reviewID string) (*RoutableAnalysis, error) {
	var out RoutableAnalysis
	var summary, storeName *string
	err := s.Pool.QueryRow(ctx, `
		SELECT a.id, v.id, a.risk_level, a.summary, v.source_url, st.name
		FROM reviews v
		JOIN analysis_results a ON a.review_id = v.id AND a.is_current AND a.deleted_at IS NULL
		LEFT JOIN stores st ON st.id = v.store_id AND st.deleted_at IS NULL
		WHERE v.id = $1 AND v.deleted_at IS NULL`, reviewID).
		Scan(&out.AnalysisID, &out.ReviewID, &out.RiskLevel, &summary, &out.SourceURL, &storeName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // 已刪除或尚無分析 → caller 略過
	}
	if err != nil {
		return nil, err
	}
	if summary != nil {
		out.Summary = *summary
	}
	if storeName != nil {
		out.StoreName = *storeName
	}
	return &out, nil
}

func (s *Store) ActiveRule(ctx context.Context, riskLevel string) (*RoutingRule, error) {
	var r RoutingRule
	err := s.Pool.QueryRow(ctx, `
		SELECT id, risk_level, assignee_roles, sla_hours, require_approval
		FROM routing_rules
		WHERE risk_level = $1 AND enabled AND deleted_at IS NULL
		ORDER BY priority LIMIT 1`, riskLevel).
		Scan(&r.ID, &r.RiskLevel, &r.AssigneeRoles, &r.SLAHours, &r.RequireApproval)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

type RouteOutcome int

const (
	RouteCreated RouteOutcome = iota
	RouteEscalated
	RouteReopened
	RouteAcknowledged // 分析指標更新但案件不動（風險未升、案件開放中）
	RouteReplay       // 同一份分析已處理過 → 只補發事件
)

func (o RouteOutcome) String() string {
	return [...]string{"created", "escalated", "reopened", "acknowledged", "replay"}[o]
}

var riskRank = map[string]int{"low": 1, "medium": 2, "high": 3}

// RouteCase 是路由核心交易：FOR UPDATE 案件列 → 決策矩陣 → 寫案件/指派/通知。
// 冪等：cases.analysis_id 指標；Replay 不重寫任何東西。
func (s *Store) RouteCase(ctx context.Context, a *RoutableAnalysis, rule *RoutingRule, now time.Time) (string, RouteOutcome, error) {
	var caseID string
	var outcome RouteOutcome
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var cur struct {
			id         string
			risk       string
			status     string
			analysisID *string
		}
		scanErr := tx.QueryRow(ctx, `
			SELECT id, risk_level, status, analysis_id FROM cases
			WHERE review_id = $1 FOR UPDATE`, a.ReviewID).
			Scan(&cur.id, &cur.risk, &cur.status, &cur.analysisID)

		if errors.Is(scanErr, pgx.ErrNoRows) {
			outcome = RouteCreated
			if err := tx.QueryRow(ctx, `
				INSERT INTO cases (review_id, risk_level, rule_id, analysis_id, status, sla_due_at)
				VALUES ($1, $2, $3, $4, 'open', $5)
				RETURNING id`,
				a.ReviewID, a.RiskLevel, rule.ID, a.AnalysisID,
				now.Add(time.Duration(rule.SLAHours)*time.Hour)).Scan(&caseID); err != nil {
				return err
			}
			return s.ensureAssignmentsAndNotify(ctx, tx, caseID, a, rule)
		}
		if scanErr != nil {
			return scanErr
		}
		caseID = cur.id

		if cur.analysisID != nil && *cur.analysisID == a.AnalysisID {
			outcome = RouteReplay // 已處理過這份分析：caller 仍補發事件（publish-loss 兜底）
			return nil
		}

		closed := cur.status == "resolved" || cur.status == "closed"
		escalating := riskRank[a.RiskLevel] > riskRank[cur.risk]
		switch {
		case closed:
			// 已結案的評論出現新分析 = 顧客又動作了 → reopen 重新計 SLA
			outcome = RouteReopened
		case escalating:
			outcome = RouteEscalated
		default:
			// 開放中且風險未升：認領新分析指標即可，案件/SLA 不動
			outcome = RouteAcknowledged
			_, err := tx.Exec(ctx,
				`UPDATE cases SET analysis_id = $2 WHERE id = $1`, caseID, a.AnalysisID)
			return err
		}

		newStatus := "open"
		reopenInc := 0
		if outcome == RouteReopened {
			reopenInc = 1
		}
		if outcome == RouteEscalated {
			newStatus = cur.status // 保持 in_progress 等既有狀態
		}
		if _, err := tx.Exec(ctx, `
			UPDATE cases SET
			    risk_level = $2, rule_id = $3, analysis_id = $4,
			    status = $5, sla_due_at = $6, sla_reminded_at = NULL,
			    reopened_count = reopened_count + $7
			WHERE id = $1`,
			caseID, a.RiskLevel, rule.ID, a.AnalysisID, newStatus,
			now.Add(time.Duration(rule.SLAHours)*time.Hour), reopenInc); err != nil {
			return err
		}
		return s.ensureAssignmentsAndNotify(ctx, tx, caseID, a, rule)
	})
	return caseID, outcome, err
}

// ensureAssignmentsAndNotify：規則要求的角色補齊指派（已存在的不重複）並排通知
func (s *Store) ensureAssignmentsAndNotify(ctx context.Context, tx pgx.Tx, caseID string, a *RoutableAnalysis, rule *RoutingRule) error {
	existing := map[string]bool{}
	rows, err := tx.Query(ctx,
		`SELECT assignee_role FROM case_assignments WHERE case_id = $1 AND deleted_at IS NULL`, caseID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			rows.Close()
			return err
		}
		existing[role] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	store := a.StoreName
	if store == "" {
		store = "未對映門市"
	}
	subject := fmt.Sprintf("【%s】負評案件（%s）", riskLabel(a.RiskLevel), store)
	body := fmt.Sprintf("%s\n原始留言：%s\nSLA：%d 小時內處理", a.Summary, a.SourceURL, rule.SLAHours)

	for _, role := range rule.AssigneeRoles {
		if !existing[role] {
			if _, err := tx.Exec(ctx, `
				INSERT INTO case_assignments (case_id, assignee_role) VALUES ($1, $2)`,
				caseID, role); err != nil {
				return err
			}
		}
		// recipient 為角色佔位（"role:xxx"），實際信箱/LINE 對象由 Notifier 解析
		if _, err := tx.Exec(ctx, `
			INSERT INTO notifications (case_id, channel, recipient, subject, body)
			VALUES ($1, 'email', $2, $3, $4)`,
			caseID, "role:"+role, subject, body); err != nil {
			return err
		}
	}
	return nil
}

func riskLabel(risk string) string {
	switch risk {
	case "high":
		return "高風險"
	case "medium":
		return "中風險"
	default:
		return "低風險"
	}
}

// FindUnroutedAnalyses：analyzed-未建案對帳（M4 審查的必解）。
// 一條判定式涵蓋「漏建案」與「漏升級/漏 reopen」：現行分析未被案件的
// analysis_id 指標認領。Acknowledged 也會更新指標，所以不會重複撿。
func (s *Store) FindUnroutedAnalyses(ctx context.Context, olderThan time.Duration, limit int) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT v.id
		FROM reviews v
		JOIN analysis_results a ON a.review_id = v.id AND a.is_current AND a.deleted_at IS NULL
		LEFT JOIN cases c ON c.review_id = v.id
		WHERE v.deleted_at IS NULL
		  AND a.created_at < now() - $1::interval
		  AND (c.id IS NULL OR c.analysis_id IS DISTINCT FROM a.id)
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

// DueSLAReminders：逾期未提醒的開放案件 → 排通知並標記（每案一次）
func (s *Store) DueSLAReminders(ctx context.Context, limit int) (int, error) {
	reminded := 0
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT c.id, c.risk_level, c.sla_due_at, v.source_url
			FROM cases c JOIN reviews v ON v.id = c.review_id
			WHERE c.status IN ('open', 'in_progress')
			  AND c.sla_due_at < now() AND c.sla_reminded_at IS NULL
			  AND c.deleted_at IS NULL
			ORDER BY c.sla_due_at LIMIT $1
			FOR UPDATE OF c SKIP LOCKED`, limit)
		if err != nil {
			return err
		}
		type due struct {
			id, risk, url string
			dueAt         time.Time
		}
		var dues []due
		for rows.Next() {
			var d due
			if err := rows.Scan(&d.id, &d.risk, &d.dueAt, &d.url); err != nil {
				rows.Close()
				return err
			}
			dues = append(dues, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, d := range dues {
			roles, err := caseRoles(ctx, tx, d.id)
			if err != nil {
				return err
			}
			for _, role := range roles {
				if _, err := tx.Exec(ctx, `
					INSERT INTO notifications (case_id, channel, recipient, subject, body)
					VALUES ($1, 'email', $2, $3, $4)`,
					d.id, "role:"+role,
					fmt.Sprintf("【SLA 逾期】%s案件待處理", riskLabel(d.risk)),
					fmt.Sprintf("SLA 期限 %s 已過，請立即處理。\n原始留言：%s",
						d.dueAt.Format(time.RFC3339), d.url)); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx,
				`UPDATE cases SET sla_reminded_at = now() WHERE id = $1`, d.id); err != nil {
				return err
			}
			reminded++
		}
		return nil
	})
	return reminded, err
}

func caseRoles(ctx context.Context, tx pgx.Tx, caseID string) ([]string, error) {
	rows, err := tx.Query(ctx,
		`SELECT assignee_role FROM case_assignments WHERE case_id = $1 AND deleted_at IS NULL`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type PendingNotification struct {
	ID        string
	Channel   string
	Recipient string
	Subject   string
	Body      string
	Retry     int
}

func (s *Store) PendingNotifications(ctx context.Context, limit int) ([]PendingNotification, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, channel, recipient, coalesce(subject, ''), coalesce(body, ''), retry_count
		FROM notifications
		WHERE status = 'pending' AND deleted_at IS NULL
		ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingNotification
	for rows.Next() {
		var n PendingNotification
		if err := rows.Scan(&n.ID, &n.Channel, &n.Recipient, &n.Subject, &n.Body, &n.Retry); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// FinishNotification：送出成功 → sent；失敗 → retry_count++（3 次後標 failed，人工介入）
func (s *Store) FinishNotification(ctx context.Context, id string, sendErr error) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if sendErr == nil {
			_, err := tx.Exec(ctx, `
				UPDATE notifications SET status = 'sent', sent_at = now(), error = NULL
				WHERE id = $1`, id)
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE notifications SET
			    retry_count = retry_count + 1,
			    error = $2,
			    status = CASE WHEN retry_count + 1 >= 3 THEN 'failed' ELSE 'pending' END
			WHERE id = $1`, id, sendErr.Error())
		return err
	})
}
