// Package bootstrap 收攏各服務 main 重複的初始化：
// logger、signal context、PostgreSQL、NATS、stream 確保。
package bootstrap

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ikala/wachen/backend/internal/envutil"
	"github.com/ikala/wachen/backend/internal/queue"
	"github.com/ikala/wachen/backend/internal/store"
)

type Service struct {
	Ctx   context.Context
	Log   *slog.Logger
	Store *store.Store
	Queue *queue.Queue
	stop  context.CancelFunc
}

// MustInit 初始化服務基座，任何一步失敗即記錄並退出。
// actor 是稽核身分（audit_logs.changed_by），例如 "svc:ingestion"。
func MustInit(svcName, actor string) *Service {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("svc", svcName)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	st, err := store.New(ctx, envutil.Must("DATABASE_URL"), actor)
	if err != nil {
		log.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	q, err := queue.New(ctx, envutil.Must("NATS_URL"))
	if err != nil {
		log.Error("nats connect failed", "err", err)
		os.Exit(1)
	}
	if err := q.EnsureStreams(ctx); err != nil {
		log.Error("ensure streams failed", "err", err)
		os.Exit(1)
	}
	return &Service{Ctx: ctx, Log: log, Store: st, Queue: q, stop: stop}
}

func (s *Service) Close() {
	s.Queue.Close()
	s.stop()
}
