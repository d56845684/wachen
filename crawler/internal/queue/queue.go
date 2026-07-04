// Package queue 封裝 NATS JetStream：
//   crawl.jobs.<adapter>  Scheduler → Crawler Workers（consumer group 分散工作）
//   review.raw            Worker → Ingestion（M3 起消費）
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

type ReviewCreatedMsg struct {
	ReviewID string `json:"review_id"`
}

func (q *Queue) PublishReviewCreated(ctx context.Context, reviewID string) error {
	data, _ := json.Marshal(ReviewCreatedMsg{ReviewID: reviewID})
	_, err := q.JS.Publish(ctx, "review.created", data)
	return err
}

// Handler：err=nil → Ack；錯誤 → 線性退避重試；達 MaxDeliver → Term。
// id 為訊息 payload 內的業務鍵（job_id / raw_review_id）。
type Handler func(ctx context.Context, id string, attempt uint64, isFinal bool) error

// consume 是兩個 durable consumer 的共用骨架：
// unmarshal → 讀投遞次數 → handler → ack / 退避 nak / term
func (q *Queue) consume(ctx context.Context, stream string, cfg jetstream.ConsumerConfig,
	extractID func([]byte) (string, error), nakBase time.Duration, handler Handler) (jetstream.ConsumeContext, error) {

	cfg.AckPolicy = jetstream.AckExplicitPolicy
	cfg.MaxDeliver = MaxDeliver
	cons, err := q.JS.CreateOrUpdateConsumer(ctx, stream, cfg)
	if err != nil {
		return nil, fmt.Errorf("ensure consumer %s/%s: %w", stream, cfg.Durable, err)
	}
	return cons.Consume(func(msg jetstream.Msg) {
		id, err := extractID(msg.Data())
		if err != nil {
			// 格式錯誤沒有重試的意義，但不能無聲消失
			slog.Default().Error("dropping malformed message",
				"stream", stream, "durable", cfg.Durable, "err", err)
			_ = msg.Term()
			return
		}
		attempt := uint64(1)
		if meta, err := msg.Metadata(); err == nil {
			attempt = meta.NumDelivered
		}
		isFinal := attempt >= MaxDeliver
		if err := handler(ctx, id, attempt, isFinal); err != nil {
			if isFinal {
				_ = msg.Term()
			} else {
				_ = msg.NakWithDelay(time.Duration(attempt) * nakBase)
			}
			return
		}
		_ = msg.Ack()
	})
}

// ConsumeCrawlJobs 以 durable consumer 分散消費（多 worker 共享同一 consumer）
func (q *Queue) ConsumeCrawlJobs(ctx context.Context, handler Handler) (jetstream.ConsumeContext, error) {
	return q.consume(ctx, StreamCrawl, jetstream.ConsumerConfig{
		Durable:       "crawl-workers",
		FilterSubject: "crawl.jobs.>",
		AckWait:       2 * time.Minute,
	}, func(data []byte) (string, error) {
		var m CrawlJobMsg
		err := json.Unmarshal(data, &m)
		return m.JobID, err
	}, 10*time.Second, handler)
}

// ConsumeReviewRaw 供 Ingestion 以 durable consumer 消費 review.raw
func (q *Queue) ConsumeReviewRaw(ctx context.Context, handler Handler) (jetstream.ConsumeContext, error) {
	return q.consume(ctx, StreamReviews, jetstream.ConsumerConfig{
		Durable:       "ingestion",
		FilterSubject: "review.raw",
		AckWait:       time.Minute,
	}, func(data []byte) (string, error) {
		var m ReviewRawMsg
		err := json.Unmarshal(data, &m)
		return m.RawReviewID, err
	}, 5*time.Second, handler)
}

func (q *Queue) Close() { q.nc.Close() }
