// Package service：後台 API 的業務邏輯層，介於 handler 與 store 之間。
//
// 分層：
//
//	cmd/api（handler）  — transport：路由、JSON 編解碼、JWT、HTTP 狀態碼映射
//	internal/service    — use-case：狀態白名單、稽核 actor、預設值、寫 DB 後入列
//	internal/store      — repository：SQL 與交易
//
// store 的領域錯誤（ErrInvalidTransition 等）原樣往上傳，由 handler 映射 HTTP 狀態碼。
package service

import (
	"context"
	"log/slog"

	"github.com/ikala/wachen/backend/internal/store"
)

// Store：service 對資料層的依賴（*store.Store 滿足；測試用 fake）
type Store interface {
	AuthUser(ctx context.Context, email, password string) (*store.AuthedUser, error)
	ListCases(ctx context.Context, f store.CaseFilter) ([]store.CaseSummary, error)
	CaseFacets(ctx context.Context) (stores, sources []store.Facet, err error)
	GetPipelineStats(ctx context.Context) (*store.PipelineStats, error)
	GetCaseDetail(ctx context.Context, caseID string) (*store.CaseDetail, error)
	UpdateCaseStatus(ctx context.Context, caseID, newStatus, actor string) error
	CreateReply(ctx context.Context, caseID, content, authorEmail string) (*store.Reply, bool, error)
	ApproveReply(ctx context.Context, replyID, approverEmail string) (bool, error)
	RejectReply(ctx context.Context, replyID, approverEmail, reason string) error
	PendingApprovals(ctx context.Context, limit int) ([]store.PendingReply, error)
}

// Enqueuer：建立/核准回覆後把 reply.requested 推進佇列（實作為 *queue.Queue）
type Enqueuer interface {
	PublishReplyRequested(ctx context.Context, replyID string) error
}

type Service struct {
	st  Store
	q   Enqueuer
	log *slog.Logger
}

func New(st Store, q Enqueuer, log *slog.Logger) *Service {
	return &Service{st: st, q: q, log: log}
}

// Login：帳密驗證。回 nil = 帳號不存在或密碼錯誤（不可區分，防枚舉）。
// JWT 簽發屬 transport，留在 handler。
func (s *Service) Login(ctx context.Context, email, password string) (*store.AuthedUser, error) {
	return s.st.AuthUser(ctx, email, password)
}
