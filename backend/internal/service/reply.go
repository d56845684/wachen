// 回覆的 use-case：建立/審核後的「寫 DB → 入列 reply.requested」協調集中在這層。
package service

import (
	"context"

	"github.com/ikala/wachen/backend/internal/store"
)

const approvalsLimit = 100

// CreateReply：建立回覆。低/中風險建立即 approved 並入列送出；高風險等審核。
func (s *Service) CreateReply(ctx context.Context, caseID, content, authorEmail string) (*store.Reply, error) {
	reply, enqueue, err := s.st.CreateReply(ctx, caseID, content, authorEmail)
	if err != nil {
		return nil, err
	}
	if enqueue {
		s.enqueueReply(ctx, reply.ID)
	}
	s.log.Info("reply created", "reply_id", reply.ID, "status", reply.Status, "by", authorEmail)
	return reply, nil
}

// ApproveReply：pending_approval → approved，成功即入列送出
func (s *Service) ApproveReply(ctx context.Context, replyID, approverEmail string) error {
	ok, err := s.st.ApproveReply(ctx, replyID, approverEmail)
	if err != nil {
		return err
	}
	if !ok {
		return store.ErrReplyBadState
	}
	s.enqueueReply(ctx, replyID)
	s.log.Info("reply approved", "reply_id", replyID, "by", approverEmail)
	return nil
}

func (s *Service) RejectReply(ctx context.Context, replyID, approverEmail, reason string) error {
	if err := s.st.RejectReply(ctx, replyID, approverEmail, reason); err != nil {
		return err
	}
	s.log.Info("reply rejected", "reply_id", replyID, "by", approverEmail)
	return nil
}

func (s *Service) PendingApprovals(ctx context.Context) ([]store.PendingReply, error) {
	return s.st.PendingApprovals(ctx, approvalsLimit)
}

// enqueueReply：回覆已寫入 DB（approved），入列失敗不擋使用者；
// worker 另有 approved 掃描補送（M-later）
func (s *Service) enqueueReply(ctx context.Context, replyID string) {
	if err := s.q.PublishReplyRequested(ctx, replyID); err != nil {
		s.log.Error("enqueue reply failed", "reply_id", replyID, "err", err)
	}
}
