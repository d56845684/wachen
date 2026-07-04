// Package queue 封裝 NATS JetStream：
//   crawl.jobs.<adapter>  Scheduler → Crawler Workers（consumer group 分散工作）
//   review.raw            Worker → Ingestion（M3 起消費）
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	StreamCrawl   = "CRAWL"
	StreamReviews = "REVIEWS"
	MaxDeliver    = 4 // 重試上限，超過進 dead_letter
)

type Queue struct {
	nc *nats.Conn
	JS jetstream.JetStream
}

func New(ctx context.Context, url string) (*Queue, error) {
	var nc *nats.Conn
	var err error
	for i := 0; i < 30; i++ {
		nc, err = nats.Connect(url, nats.MaxReconnects(-1))
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	return &Queue{nc: nc, JS: js}, nil
}

func (q *Queue) EnsureStreams(ctx context.Context) error {
	for _, cfg := range []jetstream.StreamConfig{
		{Name: StreamCrawl, Subjects: []string{"crawl.jobs.>"}, Retention: jetstream.WorkQueuePolicy},
		// MaxAge 防止無人消費時 volume 無限成長；M3 接上 ingestion 前的保險
		{Name: StreamReviews, Subjects: []string{"review.>"}, MaxAge: 30 * 24 * time.Hour},
	} {
		if _, err := q.JS.CreateOrUpdateStream(ctx, cfg); err != nil {
			return fmt.Errorf("ensure stream %s: %w", cfg.Name, err)
		}
	}
	return nil
}

type CrawlJobMsg struct {
	JobID string `json:"job_id"`
}

func (q *Queue) PublishCrawlJob(ctx context.Context, adapterName, jobID string) error {
	data, _ := json.Marshal(CrawlJobMsg{JobID: jobID})
	_, err := q.JS.Publish(ctx, "crawl.jobs."+adapterName, data)
	return err
}

type ReviewRawMsg struct {
	RawReviewID string `json:"raw_review_id"`
	SourceName  string `json:"source_name"`
}

func (q *Queue) PublishReviewRaw(ctx context.Context, sourceName, rawReviewID string) error {
	data, _ := json.Marshal(ReviewRawMsg{RawReviewID: rawReviewID, SourceName: sourceName})
	_, err := q.JS.Publish(ctx, "review.raw", data)
	return err
}

// JobHandler 回傳 (retryable, err)：err=nil → Ack；retryable=false 或達重試上限 → Term
type JobHandler func(ctx context.Context, jobID string, attempt uint64, isFinal bool) error

// ConsumeCrawlJobs 以 durable consumer 分散消費（多 worker 共享同一 consumer）
func (q *Queue) ConsumeCrawlJobs(ctx context.Context, handler JobHandler) (jetstream.ConsumeContext, error) {
	cons, err := q.JS.CreateOrUpdateConsumer(ctx, StreamCrawl, jetstream.ConsumerConfig{
		Durable:       "crawl-workers",
		FilterSubject: "crawl.jobs.>",
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       2 * time.Minute,
		MaxDeliver:    MaxDeliver,
	})
	if err != nil {
		return nil, fmt.Errorf("ensure consumer: %w", err)
	}
	return cons.Consume(func(msg jetstream.Msg) {
		var m CrawlJobMsg
		if err := json.Unmarshal(msg.Data(), &m); err != nil {
			_ = msg.Term() // 格式錯誤沒有重試的意義
			return
		}
		attempt := uint64(1)
		if meta, err := msg.Metadata(); err == nil {
			attempt = meta.NumDelivered
		}
		isFinal := attempt >= MaxDeliver
		if err := handler(ctx, m.JobID, attempt, isFinal); err != nil {
			if isFinal {
				_ = msg.Term()
			} else {
				_ = msg.NakWithDelay(time.Duration(attempt) * 10 * time.Second) // 退避重試
			}
			return
		}
		_ = msg.Ack()
	})
}

func (q *Queue) Close() { q.nc.Close() }
