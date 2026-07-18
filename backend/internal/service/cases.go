// 案件的 use-case：收件匣、詳情、狀態變更（人工可設定的狀態字面值把關在這層）。
package service

import (
	"context"
	"errors"

	"github.com/ikala/wachen/backend/internal/store"
)

// ErrUnknownStatus：目標狀態不在人工可設定的字面值白名單（handler 映 400）。
// 白名單內但狀態機不允許的轉換是 store.ErrInvalidTransition（handler 映 422）。
var ErrUnknownStatus = errors.New("unknown case status")

var allowedStatuses = map[string]bool{"open": true, "in_progress": true, "resolved": true, "closed": true}

const caseListLimit = 200

// ListCases：收件匣。filter 未帶 Limit 時上限 200；永遠回非 nil slice（JSON 序列化為 []）。
func (s *Service) ListCases(ctx context.Context, f store.CaseFilter) ([]store.CaseSummary, error) {
	if f.Limit <= 0 {
		f.Limit = caseListLimit
	}
	cases, err := s.st.ListCases(ctx, f)
	if err != nil {
		return nil, err
	}
	if cases == nil {
		cases = []store.CaseSummary{}
	}
	return cases, nil
}

func (s *Service) CaseFacets(ctx context.Context) (stores, sources []store.Facet, err error) {
	return s.st.CaseFacets(ctx)
}

func (s *Service) PipelineStats(ctx context.Context) (*store.PipelineStats, error) {
	return s.st.GetPipelineStats(ctx)
}

// CaseDetail：回 nil = 查無此案（handler 映 404）
func (s *Service) CaseDetail(ctx context.Context, caseID string) (*store.CaseDetail, error) {
	return s.st.GetCaseDetail(ctx, caseID)
}

// UpdateCaseStatus：稽核 actor 固定為登入使用者（user:<email>），非服務身分
func (s *Service) UpdateCaseStatus(ctx context.Context, caseID, newStatus, userEmail string) error {
	if !allowedStatuses[newStatus] {
		return ErrUnknownStatus
	}
	if err := s.st.UpdateCaseStatus(ctx, caseID, newStatus, "user:"+userEmail); err != nil {
		return err
	}
	s.log.Info("case status updated", "case", caseID, "status", newStatus, "by", userEmail)
	return nil
}
