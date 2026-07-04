package normalize

import (
	"testing"
	"time"
)

func TestGoogleNormalize(t *testing.T) {
	payload := []byte(`{
		"reviewId": "r1", "starRating": "ONE",
		"comment": "在湯裡吃到頭髮",
		"createTime": "2026-07-04T08:00:00Z",
		"reviewer": {"displayName": "黃淑芬"}
	}`)
	n, err := Normalize("google_review", payload)
	if err != nil {
		t.Fatal(err)
	}
	if n.AuthorName != "黃淑芬" || n.Content != "在湯裡吃到頭髮" {
		t.Errorf("normalized = %+v", n)
	}
	if n.Rating == nil || *n.Rating != 1 {
		t.Errorf("rating = %v, want 1", n.Rating)
	}
	want := time.Date(2026, 7, 4, 8, 0, 0, 0, time.UTC)
	if n.PostedAt == nil || !n.PostedAt.Equal(want) {
		t.Errorf("posted_at = %v", n.PostedAt)
	}
}

// 純星等無文字的評論：content 空字串仍要通過（1 星無言也是負評訊號）
func TestGoogleNormalizeRatingOnly(t *testing.T) {
	n, err := Normalize("google_review", []byte(`{"starRating": "ONE", "createTime": "2026-07-04T08:00:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	if n.Content != "" || n.Rating == nil || *n.Rating != 1 {
		t.Errorf("normalized = %+v", n)
	}
}

// 未知星等：Rating 為 nil，不猜
func TestGoogleNormalizeUnknownStar(t *testing.T) {
	n, err := Normalize("google_review", []byte(`{"starRating": "STAR_RATING_UNSPECIFIED", "comment": "x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if n.Rating != nil {
		t.Errorf("unknown star must yield nil rating, got %v", *n.Rating)
	}
}

func TestWebhookNormalize(t *testing.T) {
	payload := []byte(`{
		"author": "官網訪客", "rating": 2,
		"content": "訂位系統一直轉圈圈",
		"posted_at": "2026-07-04T09:30:00Z"
	}`)
	n, err := Normalize("webhook_generic", payload)
	if err != nil {
		t.Fatal(err)
	}
	if n.AuthorName != "官網訪客" || *n.Rating != 2 || n.Content != "訂位系統一直轉圈圈" {
		t.Errorf("normalized = %+v", n)
	}
}

// 客服管道可能沒有星等，只有文字
func TestWebhookNormalizeNoRating(t *testing.T) {
	n, err := Normalize("webhook_generic", []byte(`{"author": "客服轉入", "content": "顧客來電抱怨外送遲到"}`))
	if err != nil {
		t.Fatal(err)
	}
	if n.Rating != nil || n.Content == "" {
		t.Errorf("normalized = %+v", n)
	}
}

// 空 payload（無內容也無星等）要拒收
func TestWebhookNormalizeEmpty(t *testing.T) {
	if _, err := Normalize("webhook_generic", []byte(`{"author": "x"}`)); err == nil {
		t.Fatal("want error for empty content+rating")
	}
}

func TestUnknownAdapter(t *testing.T) {
	if _, err := Normalize("no_such_adapter", []byte(`{}`)); err == nil {
		t.Fatal("want error for unknown adapter")
	}
}
