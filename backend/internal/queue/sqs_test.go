package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// fakeSQS：一次回傳預載訊息，之後空轉；記錄 delete / visibility 呼叫。
type fakeSQS struct {
	mu       sync.Mutex
	msgs     []types.Message
	deleted  []string
	visibles map[string]int32 // receiptHandle → timeout
	sent     []string         // queue URL
}

func (f *fakeSQS) SendMessage(_ context.Context, in *awssqs.SendMessageInput, _ ...func(*awssqs.Options)) (*awssqs.SendMessageOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, aws.ToString(in.QueueUrl))
	return &awssqs.SendMessageOutput{}, nil
}

func (f *fakeSQS) ReceiveMessage(ctx context.Context, _ *awssqs.ReceiveMessageInput, _ ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error) {
	f.mu.Lock()
	msgs := f.msgs
	f.msgs = nil
	f.mu.Unlock()
	if msgs == nil {
		<-ctx.Done() // 模擬長輪詢直到取消
		return nil, ctx.Err()
	}
	return &awssqs.ReceiveMessageOutput{Messages: msgs}, nil
}

func (f *fakeSQS) DeleteMessage(_ context.Context, in *awssqs.DeleteMessageInput, _ ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, aws.ToString(in.ReceiptHandle))
	return &awssqs.DeleteMessageOutput{}, nil
}

func (f *fakeSQS) ChangeMessageVisibility(_ context.Context, in *awssqs.ChangeMessageVisibilityInput, _ ...func(*awssqs.Options)) (*awssqs.ChangeMessageVisibilityOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.visibles[aws.ToString(in.ReceiptHandle)] = in.VisibilityTimeout
	return &awssqs.ChangeMessageVisibilityOutput{}, nil
}

func msg(handle, body string, receiveCount string) types.Message {
	return types.Message{
		ReceiptHandle: aws.String(handle),
		Body:          aws.String(body),
		Attributes: map[string]string{
			string(types.MessageSystemAttributeNameApproximateReceiveCount): receiveCount,
		},
	}
}

func runConsume(t *testing.T, f *fakeSQS, handler Handler) {
	t.Helper()
	q := &SQS{client: f, urls: map[string]string{qCrawlJobs: "http://q/crawl-jobs"}}
	cc, err := q.ConsumeCrawlJobs(context.Background(), handler)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		drained := f.msgs == nil
		f.mu.Unlock()
		if drained {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // 等 handler goroutine 收尾
	cc.Stop()
}

func TestSQSAckDeletes(t *testing.T) {
	f := &fakeSQS{msgs: []types.Message{msg("h1", `{"job_id":"j1"}`, "1")}, visibles: map[string]int32{}}
	var gotID string
	var gotAttempt uint64
	runConsume(t, f, func(_ context.Context, id string, attempt uint64, isFinal bool) error {
		gotID, gotAttempt = id, attempt
		if isFinal {
			t.Error("attempt 1 must not be final")
		}
		return nil
	})
	if gotID != "j1" || gotAttempt != 1 {
		t.Fatalf("handler got (%s, %d), want (j1, 1)", gotID, gotAttempt)
	}
	if len(f.deleted) != 1 || f.deleted[0] != "h1" {
		t.Fatalf("expected delete of h1, got %v", f.deleted)
	}
}

func TestSQSRetryBacksOff(t *testing.T) {
	f := &fakeSQS{msgs: []types.Message{msg("h1", `{"job_id":"j1"}`, "2")}, visibles: map[string]int32{}}
	runConsume(t, f, func(_ context.Context, _ string, _ uint64, _ bool) error {
		return errors.New("boom")
	})
	// attempt=2 × nakBase 10s = 20s
	if got := f.visibles["h1"]; got != 20 {
		t.Fatalf("visibility = %d, want 20", got)
	}
	if len(f.deleted) != 0 {
		t.Fatalf("failed message must not be deleted, got %v", f.deleted)
	}
}

func TestSQSFinalAttemptReleasesToDLQ(t *testing.T) {
	f := &fakeSQS{msgs: []types.Message{msg("h1", `{"job_id":"j1"}`, "4")}, visibles: map[string]int32{}}
	var wasFinal bool
	runConsume(t, f, func(_ context.Context, _ string, _ uint64, isFinal bool) error {
		wasFinal = isFinal
		return errors.New("boom")
	})
	if !wasFinal {
		t.Fatal("attempt 4 (= MaxDeliver) must be final")
	}
	if got := f.visibles["h1"]; got != 0 {
		t.Fatalf("final failure should release with visibility 0 (DLQ path), got %d", got)
	}
	if len(f.deleted) != 0 {
		t.Fatal("final failure must not delete (redrive moves it to DLQ)")
	}
}

func TestSQSMalformedReleasesToDLQ(t *testing.T) {
	f := &fakeSQS{msgs: []types.Message{msg("h1", `not-json`, "1")}, visibles: map[string]int32{}}
	called := false
	runConsume(t, f, func(_ context.Context, _ string, _ uint64, _ bool) error {
		called = true
		return nil
	})
	if called {
		t.Fatal("handler must not run for malformed message")
	}
	if got, ok := f.visibles["h1"]; !ok || got != 0 {
		t.Fatalf("malformed should release with visibility 0, got %v (present=%v)", got, ok)
	}
}
