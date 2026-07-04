// Package google 實作 Google Business Profile 評論的抓取與回覆。
// 評論端點目前仍在 Google My Business API v4（mybusiness.googleapis.com）。
// 測試時把 sources.config 的 api_base_url 指向 mockgoogle 即可，程式碼不變。
//
// 注意：真實 v4 API 沒有單則評論的 permalink 欄位——
// SourceURL 由 adapter 用 place_id 組出「該店評論頁」deep link（T2-A），
// 不依賴任何 mock 才有的欄位。
package google

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ikala/wachen/backend/internal/adapter"
)

const (
	defaultBaseURL = "https://mybusiness.googleapis.com"
	maxPages       = 20 // 單次任務分頁安全上限；命中會回報 PageCapHit
	cursorKey      = "last_update_time"
)

type Config struct {
	APIBaseURL  string   `json:"api_base_url"`
	AccountID   string   `json:"account_id"`   // "accounts/123"
	LocationIDs []string `json:"location_ids"` // scheduler 派工用；Fetch 只看 job.LocationID
	MaxRating   float64  `json:"max_rating"`   // 只收 <= 此星等（截圖：<3 星負評），預設 3
}

type Adapter struct {
	HTTP *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func New() *Adapter {
	return &Adapter{HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (a *Adapter) Name() string { return "google_review" }

type gReview struct {
	ReviewID   string    `json:"reviewId"`
	Name       string    `json:"name"`
	StarRating string    `json:"starRating"` // ONE..FIVE
	Comment    string    `json:"comment"`
	CreateTime time.Time `json:"createTime"`
	UpdateTime time.Time `json:"updateTime"`
	Reviewer   struct {
		DisplayName string `json:"displayName"`
	} `json:"reviewer"`
}

type listResp struct {
	Reviews       []json.RawMessage `json:"reviews"`
	NextPageToken string            `json:"nextPageToken"`
}

var starToNum = map[string]float64{"ONE": 1, "TWO": 2, "THREE": 3, "FOUR": 4, "FIVE": 5}

// reviewPageURL 組「該店評論頁」官方 deep link——真 v4 沒有單則 permalink
func reviewPageURL(placeID string) string {
	return "https://search.google.com/local/reviews?placeid=" + url.QueryEscape(placeID)
}

// Fetch 增量抓取單一 location：cursor 記最後 updateTime，
// 明確以 orderBy=updateTime desc 請求，抓到已見過的時間點即停。
func (a *Adapter) Fetch(ctx context.Context, job adapter.CrawlJob) (*adapter.FetchResult, error) {
	var cfg Config
	if err := json.Unmarshal(job.Config, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	base := strings.TrimRight(cfg.APIBaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	if err := a.checkCredentials(base); err != nil {
		return nil, err
	}
	if job.LocationID == "" {
		return nil, fmt.Errorf("job has no location_id")
	}
	if job.PlaceID == "" {
		// source_url 是硬需求（一鍵跳回原頁），沒有對映寧可明確失敗
		return nil, fmt.Errorf("no place_id for %s: add a stores row with google_location_id", job.LocationID)
	}
	maxRating := cfg.MaxRating
	if maxRating == 0 {
		maxRating = 3
	}

	since := cursorTime(job.Cursor)
	newest := since
	pageToken := ""
	res := &adapter.FetchResult{}

pages:
	for page := 0; ; page++ {
		if page == maxPages {
			res.PageCapHit = true // 不靜默：worker 會寫進 stats 並記 warning
			break
		}
		var lr listResp
		u := fmt.Sprintf("%s/v4/%s/%s/reviews?pageSize=50&orderBy=%s&pageToken=%s",
			base, cfg.AccountID, job.LocationID,
			url.QueryEscape("updateTime desc"), url.QueryEscape(pageToken))
		if err := a.getJSON(ctx, u, &lr); err != nil {
			return nil, fmt.Errorf("list reviews %s: %w", job.LocationID, err)
		}
		for _, raw := range lr.Reviews {
			var r gReview
			if err := json.Unmarshal(raw, &r); err != nil {
				continue
			}
			if !r.UpdateTime.After(since) {
				break pages // 依 updateTime desc 排序，之後都是舊資料
			}
			if r.UpdateTime.After(newest) {
				newest = r.UpdateTime
			}
			rating, known := starToNum[r.StarRating]
			if !known {
				// 未知星等不能靜默當 0 分收進來；跳過並讓 worker 記 warning
				continue
			}
			if rating > maxRating {
				continue // 星等高於門檻，不是負評
			}
			sum := sha256.Sum256(raw)
			res.Reviews = append(res.Reviews, adapter.RawReview{
				SourceName:  job.SourceName,
				ExternalID:  r.ReviewID,
				Payload:     raw,
				ContentHash: hex.EncodeToString(sum[:]),
				SourceURL:   reviewPageURL(job.PlaceID),
				LocationID:  job.LocationID,
				FetchedAt:   time.Now().UTC(),
			})
		}
		if lr.NextPageToken == "" {
			break
		}
		pageToken = lr.NextPageToken
	}
	res.Cursor = adapter.Cursor{cursorKey: newest.Format(time.RFC3339Nano)}
	return res, nil
}

// Reply 回覆評論：一則評論僅一個商家回覆，重送即覆蓋（M7 使用）。
// req.LocationID 已知時直打；未知時退回掃描 config 內的 locations。
func (a *Adapter) Reply(ctx context.Context, rawCfg json.RawMessage, req adapter.ReplyRequest) (*adapter.ReplyResult, error) {
	var cfg Config
	if err := json.Unmarshal(rawCfg, &cfg); err != nil {
		return nil, err
	}
	base := strings.TrimRight(cfg.APIBaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	if err := a.checkCredentials(base); err != nil {
		return nil, err
	}
	locations := cfg.LocationIDs
	if req.LocationID != "" {
		locations = []string{req.LocationID}
	}
	for _, loc := range locations {
		u := fmt.Sprintf("%s/v4/%s/%s/reviews/%s/reply", base, cfg.AccountID, loc, req.ExternalID)
		body, _ := json.Marshal(map[string]string{"comment": req.Content})
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, u, strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if err := a.authorize(ctx, httpReq); err != nil {
			return nil, err
		}
		resp, err := a.HTTP.Do(httpReq)
		if err != nil {
			return nil, err
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			continue // 換下一個 location
		}
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("reply %s: status %d: %s", u, resp.StatusCode, data)
		}
		return &adapter.ReplyResult{ExternalReplyID: req.ExternalID + "/reply"}, nil
	}
	return nil, fmt.Errorf("review %s not found in any configured location", req.ExternalID)
}

func (a *Adapter) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if err := a.authorize(ctx, req); err != nil {
		return err
	}
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("GET %s: status %d: %s", u, resp.StatusCode, data)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// checkCredentials：打真端點卻沒有任何憑證 → 立即失敗，
// 絕不靜默降級成匿名請求（部署設定錯誤要在第一次抓取就爆出來）
func (a *Adapter) checkCredentials(base string) error {
	if base != defaultBaseURL {
		return nil // mock / 測試端點
	}
	if os.Getenv("GOOGLE_ACCESS_TOKEN") == "" && os.Getenv("GOOGLE_REFRESH_TOKEN") == "" {
		return fmt.Errorf("real Google API requires GOOGLE_ACCESS_TOKEN or GOOGLE_REFRESH_TOKEN; refusing anonymous requests")
	}
	return nil
}

// authorize 依環境變數決定認證方式：
//   GOOGLE_ACCESS_TOKEN                          → 固定 bearer（測試用）
//   GOOGLE_CLIENT_ID/SECRET + GOOGLE_REFRESH_TOKEN → refresh token 換 access token
//   都沒有                                        → 不帶認證（僅限非預設端點，見 checkCredentials）
func (a *Adapter) authorize(ctx context.Context, req *http.Request) error {
	if t := os.Getenv("GOOGLE_ACCESS_TOKEN"); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
		return nil
	}
	if os.Getenv("GOOGLE_REFRESH_TOKEN") == "" {
		return nil
	}
	tok, err := a.refreshedToken(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return nil
}

func (a *Adapter) refreshedToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.accessToken != "" && time.Now().Before(a.tokenExpiry) {
		return a.accessToken, nil
	}
	endpoint := os.Getenv("GOOGLE_TOKEN_URL") // 測試注入用；未設定走官方
	if endpoint == "" {
		endpoint = "https://oauth2.googleapis.com/token"
	}
	form := url.Values{
		"client_id":     {os.Getenv("GOOGLE_CLIENT_ID")},
		"client_secret": {os.Getenv("GOOGLE_CLIENT_SECRET")},
		"refresh_token": {os.Getenv("GOOGLE_REFRESH_TOKEN")},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", err
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("oauth refresh failed: status %d", resp.StatusCode)
	}
	a.accessToken = tr.AccessToken
	a.tokenExpiry = time.Now().Add(time.Duration(tr.ExpiresIn-60) * time.Second)
	return a.accessToken, nil
}

func cursorTime(c adapter.Cursor) time.Time {
	if c == nil {
		return time.Time{}
	}
	if s, ok := c[cursorKey].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
