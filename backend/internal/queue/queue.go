// Package queue 抽象事件層，兩個實作以 QUEUE_DRIVER 切換：
//
//	nats（預設）  NATS JetStream（PoC docker-compose）
//	sqs          AWS SQS（正式環境，佇列由 deploy/aws/ Terraform 建立）
//
// 事件流：
//
//	crawl.jobs.<adapter>  Scheduler → Crawler Workers（consumer group 分散工作）
//	review.raw            Worker/Webhook → Ingestion
//	review.created        Ingestion → Analyzer（Python）
//	review.analyzed       Analyzer → Routing
//	case.created          Routing → 下游
//	reply.requested       API → Reply Worker
package queue

import (
	"context"

	"github.com/ikala/wachen/backend/internal/envutil"
)

const MaxDeliver = 4 // 重試上限，超過進 dead-letter（SQS redrive maxReceiveCount 須等於此值）

// Handler：err=nil → Ack；錯誤 → 線性退避重試；達 MaxDeliver → Term。
// id 為訊息 payload 內的業務鍵（job_id / raw_review_id）。
type Handler func(ctx context.Context, id string, attempt uint64, isFinal bool) error

// Consumer 是啟動後的消費迴圈，Stop 停止拉取。
type Consumer interface {
	Stop()
}

type Queue interface {
	// EnsureStreams 建立佇列拓撲（NATS）；SQS 由 Terraform 建立，為 no-op。
	EnsureStreams(ctx context.Context) error

	PublishCrawlJob(ctx context.Context, adapterName, jobID string) error
	PublishReviewRaw(ctx context.Context, sourceName, rawReviewID string) error
	PublishReviewCreated(ctx context.Context, reviewID string) error
	PublishCaseEvent(ctx context.Context, m CaseEventMsg) error
	PublishReplyRequested(ctx context.Context, replyID string) error

	ConsumeCrawlJobs(ctx context.Context, handler Handler) (Consumer, error)
	ConsumeReviewRaw(ctx context.Context, handler Handler) (Consumer, error)
	ConsumeReviewAnalyzed(ctx context.Context, handler Handler) (Consumer, error)
	ConsumeReplyRequested(ctx context.Context, handler Handler) (Consumer, error)

	Close()
}

// NewFromEnv 依 QUEUE_DRIVER 建立實作（nats 需 NATS_URL；sqs 需 SQS_*_URL 與 AWS 憑證鏈）。
func NewFromEnv(ctx context.Context) (Queue, error) {
	if envutil.Or("QUEUE_DRIVER", "nats") == "sqs" {
		return NewSQS(ctx)
	}
	return New(ctx, envutil.Must("NATS_URL"))
}

type CrawlJobMsg struct {
	JobID string `json:"job_id"`
}

type ReviewRawMsg struct {
	RawReviewID string `json:"raw_review_id"`
	SourceName  string `json:"source_name"`
}

type ReviewCreatedMsg struct {
	ReviewID string `json:"review_id"`
}

// ReviewAnalyzedMsg：依 M5 契約，payload 僅是提示——Routing 只取 review_id，
// 一律重讀 is_current 分析，不信 payload 裡的 risk_level
type ReviewAnalyzedMsg struct {
	ReviewID string `json:"review_id"`
}

type ReplyRequestedMsg struct {
	ReplyID string `json:"reply_id"`
}

type CaseEventMsg struct {
	CaseID     string `json:"case_id"`
	ReviewID   string `json:"review_id"`
	AnalysisID string `json:"analysis_id"` // 下游冪等鍵
	RiskLevel  string `json:"risk_level"`
	Action     string `json:"action"` // created / escalated / reopened / replay
}
