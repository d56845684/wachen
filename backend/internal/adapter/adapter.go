package adapter

import (
	"context"
	"encoding/json"
	"time"
)

// Cursor 為增量抓取狀態，內容由各 adapter 自行定義（存於 crawl_jobs.cursor_state）
type Cursor map[string]any

// CrawlJob 的粒度是「一個 source 的一個 location」——
// 500 家連鎖 = 500 個可平行的小任務，worker 才能真正分食（T5-A）
type CrawlJob struct {
	ID         string
	SourceID   string
	SourceName string
	Adapter    string
	Config     json.RawMessage // sources.config
	LocationID string          // "locations/123"；無 location 概念的來源留空
	PlaceID    string          // 由 stores 表 join 而來，deep link 用（T2-A）
	Cursor     Cursor          // 上次成功任務的 cursor，首次為 nil
}

type RawReview struct {
	SourceName  string
	ExternalID  string
	Payload     json.RawMessage
	ContentHash string
	SourceURL   string // 該則留言的 permalink 或該店評論頁 deep link（必填）
	LocationID  string
	FetchedAt   time.Time
}

// FetchResult 除了資料本身，也回報抓取品質訊號（不靜默截斷）
type FetchResult struct {
	Reviews    []RawReview
	Cursor     Cursor
	PageCapHit bool // 命中分頁安全上限，尾端可能有未抓資料（3A）
}

type ReplyRequest struct {
	ExternalID     string
	LocationID     string // 已知歸屬時直接指定，不用掃描
	Content        string
	IdempotencyKey string
}

type ReplyResult struct {
	ExternalReplyID string
	ReplyURL        string
}

type SourceAdapter interface {
	Name() string
	Fetch(ctx context.Context, job CrawlJob) (*FetchResult, error)
}

// 支援回覆的來源額外實作此介面（Reply Worker 以 type assertion 偵測）
type ReplyCapable interface {
	Reply(ctx context.Context, cfg json.RawMessage, req ReplyRequest) (*ReplyResult, error)
}

var registry = map[string]SourceAdapter{}

func Register(a SourceAdapter)              { registry[a.Name()] = a }
func Get(name string) (SourceAdapter, bool) { a, ok := registry[name]; return a, ok }
