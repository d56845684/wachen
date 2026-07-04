package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T, seedPerLoc int) (*server, *httptest.Server) {
	t.Helper()
	s := &server{
		byLoc:   map[string][]*review{},
		account: "accounts/mock-account",
		log:     slog.New(slog.NewJSONHandler(os.Stderr, nil)),
	}
	s.mu.Lock()
	base := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	for i := 0; i < seedPerLoc; i++ {
		r := s.addRandomReview("locations/mock-loc-1")
		r.UpdateTime = base.Add(time.Duration(i) * time.Hour)
		r.CreateTime = r.UpdateTime
	}
	s.mu.Unlock()
	ts := httptest.NewServer(http.HandlerFunc(s.route))
	t.Cleanup(ts.Close)
	return s, ts
}

func getList(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// 回應必須依 updateTime 由新到舊（google adapter 的增量邏輯依賴這點）
func TestListOrderedByUpdateTimeDesc(t *testing.T) {
	_, ts := newTestServer(t, 5)
	out := getList(t, ts.URL+"/v4/accounts/mock-account/locations/mock-loc-1/reviews")

	reviews := out["reviews"].([]any)
	if len(reviews) != 5 {
		t.Fatalf("want 5 reviews, got %d", len(reviews))
	}
	prev := time.Time{}
	for i, raw := range reviews {
		ut, _ := time.Parse(time.RFC3339Nano, raw.(map[string]any)["updateTime"].(string))
		if i > 0 && ut.After(prev) {
			t.Fatalf("reviews not sorted desc at index %d", i)
		}
		prev = ut
	}
	if int(out["totalReviewCount"].(float64)) != 5 {
		t.Errorf("totalReviewCount = %v", out["totalReviewCount"])
	}
}

func TestListPagination(t *testing.T) {
	_, ts := newTestServer(t, 5)
	base := ts.URL + "/v4/accounts/mock-account/locations/mock-loc-1/reviews?pageSize=2"

	page1 := getList(t, base)
	if n := len(page1["reviews"].([]any)); n != 2 {
		t.Fatalf("page1 size = %d", n)
	}
	tok, ok := page1["nextPageToken"].(string)
	if !ok || tok == "" {
		t.Fatal("missing nextPageToken on page1")
	}

	page2 := getList(t, base+"&pageToken="+tok)
	if n := len(page2["reviews"].([]any)); n != 2 {
		t.Fatalf("page2 size = %d", n)
	}

	tok2 := page2["nextPageToken"].(string)
	page3 := getList(t, base+"&pageToken="+tok2)
	if n := len(page3["reviews"].([]any)); n != 1 {
		t.Fatalf("page3 size = %d", n)
	}
	if _, has := page3["nextPageToken"]; has {
		t.Error("last page should not have nextPageToken")
	}

	// 三頁合計不重複
	seen := map[string]bool{}
	for _, p := range []map[string]any{page1, page2, page3} {
		for _, raw := range p["reviews"].([]any) {
			id := raw.(map[string]any)["reviewId"].(string)
			if seen[id] {
				t.Errorf("duplicate review %s across pages", id)
			}
			seen[id] = true
		}
	}
}

// mock 的欄位集合不能比真 API 好心——不得提供 reviewUrl 之類捏造欄位
func TestNoFabricatedFields(t *testing.T) {
	_, ts := newTestServer(t, 3)
	out := getList(t, ts.URL+"/v4/accounts/mock-account/locations/mock-loc-1/reviews")
	for _, raw := range out["reviews"].([]any) {
		r := raw.(map[string]any)
		if _, has := r["reviewUrl"]; has {
			t.Errorf("review %v has fabricated field reviewUrl (real v4 API has no permalink)", r["reviewId"])
		}
	}
}

// 編輯模擬：內容改變、星等下降、updateTime 前進——版本化抓取（T1-A）的測試素材
func TestEditRandomReview(t *testing.T) {
	s, _ := newTestServer(t, 1)
	s.mu.Lock()
	orig := *s.byLoc["locations/mock-loc-1"][0]
	edited := s.editRandomReview("locations/mock-loc-1")
	s.mu.Unlock()

	if edited == nil {
		t.Fatal("edit returned nil with seeded reviews")
	}
	if edited.Comment == orig.Comment {
		t.Error("comment must change")
	}
	if !edited.UpdateTime.After(orig.UpdateTime) {
		t.Error("updateTime must advance (incremental fetch relies on it)")
	}
	if orig.StarRating != "ONE" && edited.StarRating == orig.StarRating {
		t.Error("star rating should drop")
	}
	if edited.ReviewID != orig.ReviewID {
		t.Error("edit must keep the same reviewId")
	}
}

func TestEditOnEmptyLocation(t *testing.T) {
	s, _ := newTestServer(t, 0)
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.editRandomReview("locations/mock-loc-1"); r != nil {
		t.Fatal("edit on empty location must return nil")
	}
}

func TestPutReply(t *testing.T) {
	s, ts := newTestServer(t, 1)
	s.mu.Lock()
	id := s.byLoc["locations/mock-loc-1"][0].ReviewID
	s.mu.Unlock()

	url := fmt.Sprintf("%s/v4/accounts/mock-account/locations/mock-loc-1/reviews/%s/reply", ts.URL, id)
	req, _ := http.NewRequest(http.MethodPut, url, strings.NewReader(`{"comment": "抱歉造成不便，已通知店主管改善"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	// 回覆要出現在後續 list 的 reviewReply 欄位
	out := getList(t, ts.URL+"/v4/accounts/mock-account/locations/mock-loc-1/reviews")
	r := out["reviews"].([]any)[0].(map[string]any)
	reply, _ := r["reviewReply"].(map[string]any)
	if reply == nil || reply["comment"] != "抱歉造成不便，已通知店主管改善" {
		t.Errorf("reviewReply = %v", r["reviewReply"])
	}
}

func TestPutReplyNotFound(t *testing.T) {
	_, ts := newTestServer(t, 1)
	url := ts.URL + "/v4/accounts/mock-account/locations/mock-loc-1/reviews/no-such-id/reply"
	req, _ := http.NewRequest(http.MethodPut, url, strings.NewReader(`{"comment": "x"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
