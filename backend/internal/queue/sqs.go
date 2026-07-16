package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/ikala/wachen/backend/internal/envutil"
)

// sqsAPI 只列用到的方法，測試以 fake 替身注入。
type sqsAPI interface {
	SendMessage(ctx context.Context, in *awssqs.SendMessageInput, opts ...func(*awssqs.Options)) (*awssqs.SendMessageOutput, error)
	ReceiveMessage(ctx context.Context, in *awssqs.ReceiveMessageInput, opts ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, in *awssqs.DeleteMessageInput, opts ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error)
	ChangeMessageVisibility(ctx context.Context, in *awssqs.ChangeMessageVisibilityInput, opts ...func(*awssqs.Options)) (*awssqs.ChangeMessageVisibilityOutput, error)
}

// SQS 實作。每個事件一條佇列（URL 由環境變數注入，佇列由 deploy/aws/ Terraform 建立）。
// 語意對映 JetStream：
//
//	Ack           → DeleteMessage
//	NakWithDelay  → ChangeMessageVisibility(delay)
//	Term          → ChangeMessageVisibility(0)，燒完 maxReceiveCount 後由 redrive 移入 DLQ
//	                （redrive maxReceiveCount 必須 = MaxDeliver，deploy/aws/queues.tf 對齊）
type SQS struct {
	client sqsAPI
	urls   map[string]string
}

var _ Queue = (*SQS)(nil)

const (
	qCrawlJobs      = "SQS_CRAWL_JOBS_URL"
	qReviewRaw      = "SQS_REVIEW_RAW_URL"
	qReviewCreated  = "SQS_REVIEW_CREATED_URL"
	qReviewAnalyzed = "SQS_REVIEW_ANALYZED_URL"
	qCaseCreated    = "SQS_CASE_CREATED_URL"
	qReplyRequested = "SQS_REPLY_REQUESTED_URL"
)

func NewSQS(ctx context.Context) (*SQS, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	urls := map[string]string{}
	for _, key := range []string{qCrawlJobs, qReviewRaw, qReviewCreated, qReviewAnalyzed, qCaseCreated, qReplyRequested} {
		urls[key] = envutil.Must(key)
	}
	return &SQS{client: awssqs.NewFromConfig(cfg), urls: urls}, nil
}

// EnsureStreams：佇列由 Terraform 建立，這裡無事可做。
func (q *SQS) EnsureStreams(context.Context) error { return nil }

func (q *SQS) publish(ctx context.Context, urlKey string, v any) error {
	data, _ := json.Marshal(v)
	_, err := q.client.SendMessage(ctx, &awssqs.SendMessageInput{
		QueueUrl:    aws.String(q.urls[urlKey]),
		MessageBody: aws.String(string(data)),
	})
	return err
}

// adapterName 不進路由：單一佇列所有 adapter 共用，worker 依 job 內容分派（同 NATS 的 crawl.jobs.> 消費行為）
func (q *SQS) PublishCrawlJob(ctx context.Context, _, jobID string) error {
	return q.publish(ctx, qCrawlJobs, CrawlJobMsg{JobID: jobID})
}

func (q *SQS) PublishReviewRaw(ctx context.Context, sourceName, rawReviewID string) error {
	return q.publish(ctx, qReviewRaw, ReviewRawMsg{RawReviewID: rawReviewID, SourceName: sourceName})
}

func (q *SQS) PublishReviewCreated(ctx context.Context, reviewID string) error {
	return q.publish(ctx, qReviewCreated, ReviewCreatedMsg{ReviewID: reviewID})
}

func (q *SQS) PublishCaseEvent(ctx context.Context, m CaseEventMsg) error {
	return q.publish(ctx, qCaseCreated, m)
}

func (q *SQS) PublishReplyRequested(ctx context.Context, replyID string) error {
	return q.publish(ctx, qReplyRequested, ReplyRequestedMsg{ReplyID: replyID})
}

func (q *SQS) ConsumeCrawlJobs(ctx context.Context, handler Handler) (Consumer, error) {
	return q.consume(ctx, qCrawlJobs, func(data []byte) (string, error) {
		var m CrawlJobMsg
		err := json.Unmarshal(data, &m)
		return m.JobID, err
	}, 10*time.Second, handler)
}

func (q *SQS) ConsumeReviewRaw(ctx context.Context, handler Handler) (Consumer, error) {
	return q.consume(ctx, qReviewRaw, func(data []byte) (string, error) {
		var m ReviewRawMsg
		err := json.Unmarshal(data, &m)
		return m.RawReviewID, err
	}, 5*time.Second, handler)
}

func (q *SQS) ConsumeReviewAnalyzed(ctx context.Context, handler Handler) (Consumer, error) {
	return q.consume(ctx, qReviewAnalyzed, func(data []byte) (string, error) {
		var m ReviewAnalyzedMsg
		err := json.Unmarshal(data, &m)
		return m.ReviewID, err
	}, 5*time.Second, handler)
}

func (q *SQS) ConsumeReplyRequested(ctx context.Context, handler Handler) (Consumer, error) {
	return q.consume(ctx, qReplyRequested, func(data []byte) (string, error) {
		var m ReplyRequestedMsg
		err := json.Unmarshal(data, &m)
		return m.ReplyID, err
	}, 5*time.Second, handler)
}

type sqsConsumer struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (c *sqsConsumer) Stop() {
	c.cancel()
	<-c.done
}

// consume：長輪詢迴圈。批次內每則訊息各自 goroutine（對齊 JetStream Consume 的並行行為），
// handler 冪等性是系統設計前提（版本化去重 / input_hash / idempotency_key）。
func (q *SQS) consume(ctx context.Context, urlKey string,
	extractID func([]byte) (string, error), nakBase time.Duration, handler Handler) (Consumer, error) {

	url := q.urls[urlKey]
	cctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			out, err := q.client.ReceiveMessage(cctx, &awssqs.ReceiveMessageInput{
				QueueUrl:              aws.String(url),
				MaxNumberOfMessages:   10,
				WaitTimeSeconds:       20,
				MessageSystemAttributeNames: []types.MessageSystemAttributeName{
					types.MessageSystemAttributeNameApproximateReceiveCount,
				},
			})
			if err != nil {
				if cctx.Err() != nil {
					return
				}
				slog.Default().Error("sqs receive failed", "queue", urlKey, "err", err)
				select {
				case <-cctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
				continue
			}
			var wg sync.WaitGroup
			for _, m := range out.Messages {
				wg.Add(1)
				go func(m types.Message) {
					defer wg.Done()
					q.handleOne(cctx, url, urlKey, m, extractID, nakBase, handler)
				}(m)
			}
			wg.Wait()
		}
	}()
	return &sqsConsumer{cancel: cancel, done: done}, nil
}

func (q *SQS) handleOne(ctx context.Context, url, urlKey string, m types.Message,
	extractID func([]byte) (string, error), nakBase time.Duration, handler Handler) {

	id, err := extractID([]byte(aws.ToString(m.Body)))
	if err != nil {
		slog.Default().Error("malformed message, releasing to DLQ path", "queue", urlKey, "err", err)
		q.term(ctx, url, m)
		return
	}
	attempt := uint64(1)
	if n, err := strconv.ParseUint(m.Attributes[string(types.MessageSystemAttributeNameApproximateReceiveCount)], 10, 64); err == nil {
		attempt = n
	}
	isFinal := attempt >= MaxDeliver
	if err := handler(ctx, id, attempt, isFinal); err != nil {
		if isFinal {
			q.term(ctx, url, m)
		} else {
			_, _ = q.client.ChangeMessageVisibility(ctx, &awssqs.ChangeMessageVisibilityInput{
				QueueUrl:          aws.String(url),
				ReceiptHandle:     m.ReceiptHandle,
				VisibilityTimeout: int32((time.Duration(attempt) * nakBase).Seconds()),
			})
		}
		return
	}
	_, _ = q.client.DeleteMessage(ctx, &awssqs.DeleteMessageInput{
		QueueUrl:      aws.String(url),
		ReceiptHandle: m.ReceiptHandle,
	})
}

// term：立即釋放訊息，讓 redrive（maxReceiveCount=MaxDeliver）把它移入 DLQ 供人工檢視。
func (q *SQS) term(ctx context.Context, url string, m types.Message) {
	_, _ = q.client.ChangeMessageVisibility(ctx, &awssqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(url),
		ReceiptHandle:     m.ReceiptHandle,
		VisibilityTimeout: 0,
	})
}

func (q *SQS) Close() {}
