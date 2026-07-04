// Package normalize 把各來源的原始 payload 轉成統一的 reviews 欄位。
// 新增來源 = 註冊一個 Normalizer，與 adapter 一樣的插件模式。
package normalize

import (
	"encoding/json"
	"fmt"
	"time"
)

// Normalized 是 reviews 表的正規化欄位集
type Normalized struct {
	AuthorName string
	Rating     *float64 // nil = 該來源沒有星等概念
	Content    string   // 允許空字串（純星等評論仍是負評訊號）
	PostedAt   *time.Time
}

type Normalizer func(payload []byte) (*Normalized, error)

var registry = map[string]Normalizer{}

func Register(adapterName string, n Normalizer) { registry[adapterName] = n }

// Normalize 依來源的 adapter 名稱分派
func Normalize(adapterName string, payload []byte) (*Normalized, error) {
	n, ok := registry[adapterName]
	if !ok {
		return nil, fmt.Errorf("no normalizer for adapter %q", adapterName)
	}
	return n(payload)
}

func init() {
	Register("google_review", googleNormalize)
	Register("webhook_generic", webhookNormalize)
}

var googleStars = map[string]float64{"ONE": 1, "TWO": 2, "THREE": 3, "FOUR": 4, "FIVE": 5}

// googleNormalize 解析 GBP v4 review resource
func googleNormalize(payload []byte) (*Normalized, error) {
	var r struct {
		StarRating string    `json:"starRating"`
		Comment    string    `json:"comment"`
		CreateTime time.Time `json:"createTime"`
		Reviewer   struct {
			DisplayName string `json:"displayName"`
		} `json:"reviewer"`
	}
	if err := json.Unmarshal(payload, &r); err != nil {
		return nil, fmt.Errorf("google payload: %w", err)
	}
	out := &Normalized{
		AuthorName: r.Reviewer.DisplayName,
		Content:    r.Comment,
	}
	if star, ok := googleStars[r.StarRating]; ok {
		out.Rating = &star
	}
	if !r.CreateTime.IsZero() {
		t := r.CreateTime
		out.PostedAt = &t
	}
	return out, nil
}

// webhookNormalize 解析 Webhook Gateway 定義的推送格式（官網留言/客服/NPS 匯入）
func webhookNormalize(payload []byte) (*Normalized, error) {
	var r struct {
		Author   string     `json:"author"`
		Rating   *float64   `json:"rating"`
		Content  string     `json:"content"`
		PostedAt *time.Time `json:"posted_at"`
	}
	if err := json.Unmarshal(payload, &r); err != nil {
		return nil, fmt.Errorf("webhook payload: %w", err)
	}
	if r.Content == "" && r.Rating == nil {
		return nil, fmt.Errorf("webhook payload needs content or rating")
	}
	return &Normalized{
		AuthorName: r.Author,
		Rating:     r.Rating,
		Content:    r.Content,
		PostedAt:   r.PostedAt,
	}, nil
}
