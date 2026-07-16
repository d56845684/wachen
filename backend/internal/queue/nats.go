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
	StreamCases   = "CASES"
	StreamReplies = "REPLIES"
)

// NATS 是 JetStream 實作（PoC / docker-compose 預設）。
type NATS struct {
	nc *nats.Conn
	JS jetstream.JetStream
}

var _ Queue = (*NATS)(nil)

func New(ctx context.Context, url string) (*NATS, error) {
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
	return &NATS{nc: nc, JS: js}, nil
}

func (q *NATS) EnsureStreams(ctx context.Context) error {
	for _, cfg := range []jetstream.StreamConfig{
		{Name: StreamCrawl, Subjects: []string{"crawl.jobs.>"}, Retention: jetstream.WorkQueuePolicy},
		// MaxAge 防止無人消費時 volume 無限成長；M3 接上 ingestion 前的保險
		{Name: StreamReviews, Subjects: []string{"review.>"}, MaxAge: 30 * 24 * time.Hour},
		{Name: StreamCases, Subjects: []string{"case.>"}, MaxAge: 30 * 24 * time.Hour},
		{Name: StreamReplies, Subjects: []string{"reply.>"}, MaxAge: 30 * 24 * time.Hour},
	} {
		if _, err := q.JS.CreateOrUpdateStream(ctx, cfg); err != nil {
			return fmt.Errorf("ensure stream %s: %w", cfg.Name, err)
		}
	}
	return nil
}

func (q *NATS) PublishCrawlJob(ctx context.Context, adapterName, jobID string) error {
	data, _ := json.Marshal(CrawlJobMsg{JobID: jobID})
	_, err := q.JS.Publish(ctx, "crawl.jobs."+adapterName, data)
	return err
}

func (q *NATS) PublishReviewRaw(ctx context.Context, sourceName, rawReviewID string) error {
	data, _ := json.Marshal(ReviewRawMsg{RawReviewID: rawReviewID, SourceName: sourceName})
	_, err := q.JS.Publish(ctx, "review.raw", data)
	return err
}

func (q *NATS) PublishReviewCreated(ctx context.Context, reviewID string) error {
	data, _ := json.Marshal(ReviewCreatedMsg{ReviewID: reviewID})
	_, err := q.JS.Publish(ctx, "review.created", data)
	return err
}

func (q *NATS) PublishCaseEvent(ctx context.Context, m CaseEventMsg) error {
	data, _ := json.Marshal(m)
	_, err := q.JS.Publish(ctx, "case.created", data)
	return err
}

func (q *NATS) PublishReplyRequested(ctx context.Context, replyID string) error {
	data, _ := json.Marshal(ReplyRequestedMsg{ReplyID: replyID})
	_, err := q.JS.Publish(ctx, "reply.requested", data)
	return err
}

// ConsumeCrawlJobs 以 durable consumer 分散消費（多 worker 共享同一 consumer）
func (q *NATS) ConsumeCrawlJobs(ctx context.Context, handler Handler) (Consumer, error) {
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
func (q *NATS) ConsumeReviewRaw(ctx context.Context, handler Handler) (Consumer, error) {
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

// ConsumeReviewAnalyzed 供 Routing 以 durable consumer 消費
func (q *NATS) ConsumeReviewAnalyzed(ctx context.Context, handler Handler) (Consumer, error) {
	return q.consume(ctx, StreamReviews, jetstream.ConsumerConfig{
		Durable:       "routing",
		FilterSubject: "review.analyzed",
		AckWait:       time.Minute,
	}, func(data []byte) (string, error) {
		var m ReviewAnalyzedMsg
		err := json.Unmarshal(data, &m)
		return m.ReviewID, err
	}, 5*time.Second, handler)
}

// ConsumeReplyRequested 供 Reply Worker 消費
func (q *NATS) ConsumeReplyRequested(ctx context.Context, handler Handler) (Consumer, error) {
	return q.consume(ctx, StreamReplies, jetstream.ConsumerConfig{
		Durable:       "replier",
		FilterSubject: "reply.requested",
		AckWait:       time.Minute,
	}, func(data []byte) (string, error) {
		var m ReplyRequestedMsg
		err := json.Unmarshal(data, &m)
		return m.ReplyID, err
	}, 5*time.Second, handler)
}

// consume 是 durable consumer 的共用骨架：
// unmarshal → 讀投遞次數 → handler → ack / 退避 nak / term
func (q *NATS) consume(ctx context.Context, stream string, cfg jetstream.ConsumerConfig,
	extractID func([]byte) (string, error), nakBase time.Duration, handler Handler) (Consumer, error) {

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

func (q *NATS) Close() { q.nc.Close() }
