package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/ikala/wachen/backend/internal/service"
	"github.com/ikala/wachen/backend/internal/store"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeStore 只覆寫測試用到的方法；其餘打到 embedded nil interface 會 panic（= 測試沒該打的）
type fakeStore struct {
	service.Store
	cases      []store.CaseSummary
	lastFilter store.CaseFilter
	reply      *store.Reply
	enqueue    bool
	approveOK  bool
	lastActor  string
}

func (f *fakeStore) ListCases(_ context.Context, filter store.CaseFilter) ([]store.CaseSummary, error) {
	f.lastFilter = filter
	return f.cases, nil
}
func (f *fakeStore) CreateReply(_ context.Context, _, _, _ string) (*store.Reply, bool, error) {
	return f.reply, f.enqueue, nil
}
func (f *fakeStore) ApproveReply(_ context.Context, _, _ string) (bool, error) {
	return f.approveOK, nil
}
func (f *fakeStore) UpdateCaseStatus(_ context.Context, _, _, actor string) error {
	f.lastActor = actor
	return nil
}

type fakeQueue struct {
	enqueued []string
	err      error
}

func (f *fakeQueue) PublishReplyRequested(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.enqueued = append(f.enqueued, id)
	return nil
}

func TestListCasesDefaultsLimitAndNonNil(t *testing.T) {
	st := &fakeStore{}
	svc := service.New(st, &fakeQueue{}, discard)
	cases, err := svc.ListCases(context.Background(), store.CaseFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if st.lastFilter.Limit != 200 {
		t.Errorf("limit = %d, want 200", st.lastFilter.Limit)
	}
	if cases == nil {
		t.Error("cases 必須是非 nil slice（JSON 序列化為 [] 而非 null）")
	}
}

func TestUpdateCaseStatusRejectsUnknownStatus(t *testing.T) {
	svc := service.New(&fakeStore{}, &fakeQueue{}, discard)
	err := svc.UpdateCaseStatus(context.Background(), "c1", "deleted", "a@example.com")
	if !errors.Is(err, service.ErrUnknownStatus) {
		t.Errorf("err = %v, want ErrUnknownStatus", err)
	}
}

func TestUpdateCaseStatusCarriesUserActor(t *testing.T) {
	st := &fakeStore{}
	svc := service.New(st, &fakeQueue{}, discard)
	if err := svc.UpdateCaseStatus(context.Background(), "c1", "resolved", "a@example.com"); err != nil {
		t.Fatal(err)
	}
	if st.lastActor != "user:a@example.com" {
		t.Errorf("actor = %q, want user:a@example.com", st.lastActor)
	}
}

func TestApproveReplyBadStateWhenNotOK(t *testing.T) {
	svc := service.New(&fakeStore{approveOK: false}, &fakeQueue{}, discard)
	err := svc.ApproveReply(context.Background(), "r1", "a@example.com")
	if !errors.Is(err, store.ErrReplyBadState) {
		t.Errorf("err = %v, want ErrReplyBadState", err)
	}
}

func TestApproveReplyEnqueues(t *testing.T) {
	q := &fakeQueue{}
	svc := service.New(&fakeStore{approveOK: true}, q, discard)
	if err := svc.ApproveReply(context.Background(), "r1", "a@example.com"); err != nil {
		t.Fatal(err)
	}
	if len(q.enqueued) != 1 || q.enqueued[0] != "r1" {
		t.Errorf("enqueued = %v, want [r1]", q.enqueued)
	}
}

// 入列失敗不擋使用者：回覆已寫入 DB（approved），worker 另有補送機制
func TestCreateReplyEnqueueFailureDoesNotFail(t *testing.T) {
	st := &fakeStore{reply: &store.Reply{ID: "r1", Status: "approved"}, enqueue: true}
	svc := service.New(st, &fakeQueue{err: errors.New("nats down")}, discard)
	reply, err := svc.CreateReply(context.Background(), "c1", "感謝回饋", "a@example.com")
	if err != nil || reply == nil {
		t.Fatalf("reply=%v err=%v", reply, err)
	}
}
