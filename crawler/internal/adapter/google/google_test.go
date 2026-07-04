package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikala/wachen/crawler/internal/adapter"
)

// newAPIServer 依 pageToken（頁碼字串）回傳對應頁的 JSON，並記錄收到的 query
func newAPIServer(t *testing.T, pages []string) (*httptest.Server, *[]string) {
	t.Helper()
	var queries []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/reviews") || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		queries = append(queries, r.URL.RawQuery)
		idx := 0
		if tok := r.URL.Query().Get("pageToken"); tok != "" {
			idx, _ = strconv.Atoi(tok)
		}
		if idx >= len(pages) {
			t.Errorf("unexpected pageToken %d", idx)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, pages[idx])
	}))
	return ts, &queries
}

func testJob(t *testing.T, baseURL string, cursor adapter.Cursor) adapter.CrawlJob {
	t.Helper()
	cfg, _ := json.Marshal(map[string]any{
		"api_base_url": baseURL,
		"account_id":   "accounts/a1",
		"location_ids": []string{"locations/l1"},
	})
	return adapter.CrawlJob{
		ID: "job-1", SourceName: "test_google", Adapter: "google_review",
		Config: cfg, LocationID: "locations/l1", PlaceID: "place-abc",
		Cursor: cursor,
	}
}

func review(id, star, comment, updateTime string) string {
	return fmt.Sprintf(`{
		"reviewId": %q, "name": "accounts/a1/locations/l1/reviews/%s",
		"starRating": %q, "comment": %q,
		"createTime": %q, "updateTime": %q,
		"reviewer": {"displayName": "測試者"}
	}`, id, id, star, comment, updateTime, updateTime)
}

// 首次抓取：星等過濾生效、source_url 由 place_id 組出、cursor 記到最新一筆（含被過濾的高星）
func TestFetchFiltersHighRatings(t *testing.T) {
	page := fmt.Sprintf(`{"reviews": [%s, %s, %s]}`,
		review("r1", "FIVE", "很棒", "2026-07-04T10:00:00Z"), // 高星，應被過濾
		review("r2", "ONE", "難吃", "2026-07-04T09:00:00Z"),
		review("r3", "TWO", "服務差", "2026-07-04T08:00:00Z"),
	)
	ts, _ := newAPIServer(t, []string{page})
	defer ts.Close()

	res, err := New().Fetch(context.Background(), testJob(t, ts.URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reviews) != 2 {
		t.Fatalf("want 2 reviews (FIVE filtered), got %d", len(res.Reviews))
	}
	if res.Reviews[0].ExternalID != "r2" || res.Reviews[1].ExternalID != "r3" {
		t.Errorf("unexpected ids: %v", ids(res.Reviews))
	}
	for _, r := range res.Reviews {
		// T2-A：URL 由 adapter 自己組（真 API 沒有 permalink 欄位），不依賴 mock 好心
		if r.SourceURL != "https://search.google.com/local/reviews?placeid=place-abc" {
			t.Errorf("review %s source_url = %q", r.ExternalID, r.SourceURL)
		}
		if r.LocationID != "locations/l1" {
			t.Errorf("review %s missing location_id", r.ExternalID)
		}
		if r.ContentHash == "" {
			t.Errorf("review %s missing content_hash", r.ExternalID)
		}
	}
	// cursor 應為最新 updateTime（r1 的 10:00，即使 r1 被星等過濾）
	if c := res.Cursor["last_update_time"]; c != "2026-07-04T10:00:00Z" {
		t.Errorf("cursor = %v, want 2026-07-04T10:00:00Z", c)
	}
	if res.PageCapHit {
		t.Error("single page should not hit cap")
	}
}

// 明確要求 orderBy=updateTime desc——早停邏輯不能賭隱含預設排序
func TestFetchSendsExplicitOrderBy(t *testing.T) {
	ts, queries := newAPIServer(t, []string{`{"reviews": []}`})
	defer ts.Close()

	if _, err := New().Fetch(context.Background(), testJob(t, ts.URL, nil)); err != nil {
		t.Fatal(err)
	}
	if len(*queries) == 0 || !strings.Contains((*queries)[0], "orderBy=updateTime+desc") {
		t.Errorf("query missing explicit orderBy: %v", *queries)
	}
}

// 增量抓取：只收 cursor 之後的新評論
func TestFetchIncremental(t *testing.T) {
	page := fmt.Sprintf(`{"reviews": [%s, %s, %s]}`,
		review("r4", "ONE", "新負評", "2026-07-04T11:00:00Z"),
		review("r2", "ONE", "難吃", "2026-07-04T09:00:00Z"),
		review("r3", "TWO", "服務差", "2026-07-04T08:00:00Z"),
	)
	ts, _ := newAPIServer(t, []string{page})
	defer ts.Close()

	cursor := adapter.Cursor{"last_update_time": "2026-07-04T09:00:00Z"}
	res, err := New().Fetch(context.Background(), testJob(t, ts.URL, cursor))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reviews) != 1 || res.Reviews[0].ExternalID != "r4" {
		t.Fatalf("want only r4, got %v", ids(res.Reviews))
	}
	if c := res.Cursor["last_update_time"]; c != "2026-07-04T11:00:00Z" {
		t.Errorf("cursor = %v, want 2026-07-04T11:00:00Z", c)
	}
}

// 編輯過的評論（updateTime 更新）要被重新抓到——版本化抓取的前提（T1-A）
func TestFetchPicksUpEditedReview(t *testing.T) {
	page := fmt.Sprintf(`{"reviews": [%s]}`,
		review("r2", "ONE", "難吃（更新：吃完拉肚子）", "2026-07-04T12:00:00Z"), // 舊評論被編輯
	)
	ts, _ := newAPIServer(t, []string{page})
	defer ts.Close()

	cursor := adapter.Cursor{"last_update_time": "2026-07-04T09:00:00Z"}
	res, err := New().Fetch(context.Background(), testJob(t, ts.URL, cursor))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reviews) != 1 || res.Reviews[0].ExternalID != "r2" {
		t.Fatalf("edited review must be re-fetched, got %v", ids(res.Reviews))
	}
}

// 未知星等（如 STAR_RATING_UNSPECIFIED）不能靜默當 0 分收成負評
func TestFetchSkipsUnknownStarRating(t *testing.T) {
	page := fmt.Sprintf(`{"reviews": [%s, %s]}`,
		review("r9", "STAR_RATING_UNSPECIFIED", "???", "2026-07-04T10:00:00Z"),
		review("r2", "ONE", "難吃", "2026-07-04T09:00:00Z"),
	)
	ts, _ := newAPIServer(t, []string{page})
	defer ts.Close()

	res, err := New().Fetch(context.Background(), testJob(t, ts.URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reviews) != 1 || res.Reviews[0].ExternalID != "r2" {
		t.Fatalf("unknown star must be skipped, got %v", ids(res.Reviews))
	}
}

// 分頁：跟著 nextPageToken 抓完所有頁
func TestFetchPagination(t *testing.T) {
	pages := []string{
		fmt.Sprintf(`{"reviews": [%s], "nextPageToken": "1"}`, review("p1", "ONE", "a", "2026-07-04T10:00:00Z")),
		fmt.Sprintf(`{"reviews": [%s], "nextPageToken": "2"}`, review("p2", "TWO", "b", "2026-07-04T09:00:00Z")),
		fmt.Sprintf(`{"reviews": [%s]}`, review("p3", "THREE", "c", "2026-07-04T08:00:00Z")),
	}
	ts, _ := newAPIServer(t, pages)
	defer ts.Close()

	res, err := New().Fetch(context.Background(), testJob(t, ts.URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reviews) != 3 {
		t.Fatalf("want 3 reviews across pages, got %v", ids(res.Reviews))
	}
	if res.PageCapHit {
		t.Error("3 pages should not hit cap")
	}
}

// 命中分頁上限必須回報 PageCapHit——首次同步截斷不能靜默（3A）
func TestFetchReportsPageCapHit(t *testing.T) {
	// maxPages 頁，每頁都還有 nextPageToken → 永遠抓不完
	pages := make([]string, maxPages+1)
	for i := range pages {
		ut := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Minute)
		pages[i] = fmt.Sprintf(`{"reviews": [%s], "nextPageToken": "%d"}`,
			review(fmt.Sprintf("cap%d", i), "ONE", "x", ut.Format(time.RFC3339)), i+1)
	}
	ts, _ := newAPIServer(t, pages)
	defer ts.Close()

	res, err := New().Fetch(context.Background(), testJob(t, ts.URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !res.PageCapHit {
		t.Fatal("PageCapHit must be true when pagination exceeds maxPages")
	}
	if len(res.Reviews) != maxPages {
		t.Errorf("got %d reviews, want %d", len(res.Reviews), maxPages)
	}
}

// 空回應：沒有新評論時不報錯，cursor 保持原值
func TestFetchEmpty(t *testing.T) {
	ts, _ := newAPIServer(t, []string{`{"reviews": []}`})
	defer ts.Close()

	cursor := adapter.Cursor{"last_update_time": "2026-07-04T09:00:00Z"}
	res, err := New().Fetch(context.Background(), testJob(t, ts.URL, cursor))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reviews) != 0 {
		t.Fatalf("want 0 reviews, got %d", len(res.Reviews))
	}
	if c := res.Cursor["last_update_time"]; c != "2026-07-04T09:00:00Z" {
		t.Errorf("cursor should keep original value, got %v", c)
	}
}

// 缺 place_id / location_id 是設定錯誤，要明確失敗而非產出空 source_url
func TestFetchRequiresPlaceAndLocation(t *testing.T) {
	ts, _ := newAPIServer(t, []string{`{"reviews": []}`})
	defer ts.Close()

	job := testJob(t, ts.URL, nil)
	job.PlaceID = ""
	if _, err := New().Fetch(context.Background(), job); err == nil {
		t.Fatal("want error when place_id missing")
	}

	job = testJob(t, ts.URL, nil)
	job.LocationID = ""
	if _, err := New().Fetch(context.Background(), job); err == nil {
		t.Fatal("want error when location_id missing")
	}
}

// API 錯誤要往上拋（讓 worker 重試），不能吞掉
func TestFetchAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	if _, err := New().Fetch(context.Background(), testJob(t, ts.URL, nil)); err == nil {
		t.Fatal("want error on 429, got nil")
	}
}

// 打真端點卻沒憑證 → 立即失敗，不允許匿名降級
func TestFetchFailsFastWithoutCredentials(t *testing.T) {
	t.Setenv("GOOGLE_ACCESS_TOKEN", "")
	t.Setenv("GOOGLE_REFRESH_TOKEN", "")
	cfg, _ := json.Marshal(map[string]any{"account_id": "accounts/a1"}) // 無 api_base_url → 真端點
	job := adapter.CrawlJob{Config: cfg, LocationID: "locations/l1", PlaceID: "p"}
	_, err := New().Fetch(context.Background(), job)
	if err == nil || !strings.Contains(err.Error(), "refusing anonymous") {
		t.Fatalf("want fail-fast credential error, got %v", err)
	}
}

// OAuth refresh 流程：假 token endpoint，驗證 bearer 正確帶上與快取
func TestOAuthRefreshFlow(t *testing.T) {
	tokenCalls := 0
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("bad token request: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token": "fresh-token", "expires_in": 3600}`)
	}))
	defer tokenSrv.Close()

	var gotAuth string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"reviews": []}`)
	}))
	defer apiSrv.Close()

	t.Setenv("GOOGLE_ACCESS_TOKEN", "")
	t.Setenv("GOOGLE_REFRESH_TOKEN", "rt-1")
	t.Setenv("GOOGLE_CLIENT_ID", "cid")
	t.Setenv("GOOGLE_CLIENT_SECRET", "cs")
	t.Setenv("GOOGLE_TOKEN_URL", tokenSrv.URL)

	a := New()
	if _, err := a.Fetch(context.Background(), testJob(t, apiSrv.URL, nil)); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer fresh-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	// 第二次呼叫要吃快取，不再打 token endpoint
	if _, err := a.Fetch(context.Background(), testJob(t, apiSrv.URL, nil)); err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 1 {
		t.Errorf("token endpoint called %d times, want 1 (cached)", tokenCalls)
	}
}

// Reply：req.LocationID 已知時直打，不掃描
func TestReplyDirectLocation(t *testing.T) {
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"comment": "感謝回饋"}`)
	}))
	defer ts.Close()

	cfg, _ := json.Marshal(map[string]any{
		"api_base_url": ts.URL,
		"account_id":   "accounts/a1",
		"location_ids": []string{"locations/l1", "locations/l2"},
	})
	_, err := New().Reply(context.Background(), cfg, adapter.ReplyRequest{
		ExternalID: "r9", LocationID: "locations/l2", Content: "感謝回饋",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || !strings.Contains(paths[0], "locations/l2") {
		t.Errorf("want single direct call to l2, got %v", paths)
	}
}

// Reply：未帶 location 時退回掃描，第一個 404 換下一個
func TestReplyFallbackAcrossLocations(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "locations/l1") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"comment": "ok"}`)
	}))
	defer ts.Close()

	cfg, _ := json.Marshal(map[string]any{
		"api_base_url": ts.URL,
		"account_id":   "accounts/a1",
		"location_ids": []string{"locations/l1", "locations/l2"},
	})
	res, err := New().Reply(context.Background(), cfg, adapter.ReplyRequest{ExternalID: "r9", Content: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalReplyID == "" {
		t.Error("missing external reply id")
	}
}

func TestReplyNotFoundAnywhere(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(http.NotFound))
	defer ts.Close()

	cfg, _ := json.Marshal(map[string]any{
		"api_base_url": ts.URL,
		"account_id":   "accounts/a1",
		"location_ids": []string{"locations/l1"},
	})
	if _, err := New().Reply(context.Background(), cfg, adapter.ReplyRequest{ExternalID: "nope", Content: "x"}); err == nil {
		t.Fatal("want error when review not found in any location")
	}
}

func TestCursorTimeParsing(t *testing.T) {
	if !cursorTime(nil).IsZero() {
		t.Error("nil cursor should be zero time")
	}
	want := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	if !cursorTime(adapter.Cursor{"last_update_time": "2026-07-04T10:00:00Z"}).Equal(want) {
		t.Error("cursorTime mismatch")
	}
	if !cursorTime(adapter.Cursor{"last_update_time": "garbage"}).IsZero() {
		t.Error("unparsable cursor should be zero time")
	}
}

func ids(rs []adapter.RawReview) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ExternalID
	}
	return out
}
