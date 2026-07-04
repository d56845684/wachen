// mockgoogle：模擬 Google Business Profile v4 評論 API。
// - 啟動時每個 location 預埋一批評論，之後每 MOCK_INTERVAL 秒隨機新增一則（偏向負評）
// - 每三次產生事件中約一次是「編輯既有評論」（改文字/降星、bump updateTime）——
//   驗證版本化抓取（T1-A）；欄位集合以真 API 為上限，不提供真 API 沒有的欄位
// - GET  /v4/accounts/{acc}/locations/{loc}/reviews  分頁、updateTime desc，回應格式對齊真 API
// - PUT  /v4/accounts/{acc}/locations/{loc}/reviews/{id}/reply  商家回覆（M7 用）
// 把 sources.config 的 api_base_url 指到這裡，google adapter 原封不動即可測試。
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikala/wachen/crawler/internal/envutil"
)

// review 的欄位集合對齊真實 v4 API——刻意不提供 permalink 之類真 API 沒有的欄位，
// mock 只能比現實更嚴格，不能更好心
type review struct {
	ReviewID   string         `json:"reviewId"`
	Name       string         `json:"name"`
	StarRating string         `json:"starRating"`
	Comment    string         `json:"comment"`
	CreateTime time.Time      `json:"createTime"`
	UpdateTime time.Time      `json:"updateTime"`
	Reviewer   map[string]any `json:"reviewer"`
	Reply      *reviewReply   `json:"reviewReply,omitempty"`
}

type reviewReply struct {
	Comment    string    `json:"comment"`
	UpdateTime time.Time `json:"updateTime"`
}

var stars = []string{"ONE", "TWO", "THREE", "FOUR", "FIVE"}

// 假留言庫：偏向負評（本系統就是負評追蹤），對齊截圖的分類範例
var samples = []struct {
	star int
	text string
}{
	{1, "等了快一個小時餐點才來，出餐速度有夠慢，不會再來了"},
	{1, "吃完拉肚子一整晚，懷疑食材不新鮮，要求店家給個交代"},
	{1, "店員態度超差，點餐愛理不理還翻白眼"},
	{2, "桌面油膩膩，地板黏鞋，環境清潔真的要加強"},
	{2, "漲價漲成這樣份量還變少，價格跟品質完全不成正比"},
	{2, "外送到的時候整個涼掉，湯還灑出來，包裝有夠隨便"},
	{1, "訂位系統顯示訂位成功，到現場卻說沒有紀錄，白跑一趟"},
	{2, "餐點跟照片差太多，牛肉麵裡面只有三片肉"},
	{3, "口味普通，服務也普通，就是個沒有記憶點的店"},
	{1, "在湯裡吃到頭髮，跟店員反應還被說是我自己的"},
	{2, "冷氣壞掉整間店像烤箱，用餐體驗很差"},
	{3, "餐點不錯但等太久，尖峰時段人手明顯不足"},
	{4, "整體不錯，飲料如果能少冰就更好了"},
	{5, "服務親切餐點好吃，家庭聚餐好選擇"},
	// 反諷樣本：表面詞彙正面、實際是負評——M4 情緒分析的對抗測資，
	// 靠關鍵字比對會誤判成好評，必須靠語意理解
	{1, "太厲害了，等一個半小時才上菜，這種磨練耐心的機會別家可沒有"},
	{1, "食材新鮮度令人印象深刻，昨天的魚今天還能再賣一次，真環保"},
	{2, "服務生臉臭得很有個性，愛理不理超有態度，我下次不會再來了"},
	{2, "湯頭鹹到很有誠意，喝一口要配三杯水，CP值直接翻倍"},
	{1, "訂位系統充滿驚喜，訂了跟沒訂一樣，到現場還能享受排隊的樂趣"},
	{2, "冷氣壞掉配熱騰騰的火鍋，用餐兼三溫暖，一次滿足兩個願望"},
	{3, "擺盤拍照很好看，味道嘛……建議用眼睛吃就好"},
	{1, "份量非常精緻，精緻到我拿放大鏡才找到牛肉麵裡的牛肉"},
}

var names = []string{"陳小姐", "林先生", "王大明", "張美玲", "李志豪", "黃淑芬", "吳建宏", "劉雅婷"}

type server struct {
	mu      sync.Mutex
	byLoc   map[string][]*review
	account string
	seq     int
	log     *slog.Logger
}

func (s *server) addRandomReview(loc string) *review {
	s.seq++
	sample := samples[rand.Intn(len(samples))]
	now := time.Now().UTC()
	id := fmt.Sprintf("mock-review-%06d", s.seq)
	r := &review{
		ReviewID:   id,
		Name:       fmt.Sprintf("%s/%s/reviews/%s", s.account, loc, id),
		StarRating: stars[sample.star-1],
		Comment:    sample.text,
		CreateTime: now,
		UpdateTime: now,
		Reviewer:   map[string]any{"displayName": names[rand.Intn(len(names))]},
	}
	s.byLoc[loc] = append(s.byLoc[loc], r)
	return r
}

// editRandomReview 模擬顧客編輯評論：改文字、星等降一級、bump updateTime。
// 回傳 nil 表示該 location 還沒有評論可編輯。
func (s *server) editRandomReview(loc string) *review {
	list := s.byLoc[loc]
	if len(list) == 0 {
		return nil
	}
	r := list[rand.Intn(len(list))]
	r.Comment = r.Comment + "（更新：後續處理很失望，改評價）"
	if idx := starIndex(r.StarRating); idx > 0 {
		r.StarRating = stars[idx-1] // 降一星
	}
	r.UpdateTime = time.Now().UTC()
	return r
}

func starIndex(star string) int {
	for i, v := range stars {
		if v == star {
			return i
		}
	}
	return 0
}

func (s *server) listReviews(w http.ResponseWriter, req *http.Request, loc string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := append([]*review(nil), s.byLoc[loc]...)
	sort.Slice(list, func(i, j int) bool { return list[i].UpdateTime.After(list[j].UpdateTime) })

	pageSize, _ := strconv.Atoi(req.URL.Query().Get("pageSize"))
	if pageSize <= 0 || pageSize > 50 {
		pageSize = 50
	}
	offset, _ := strconv.Atoi(req.URL.Query().Get("pageToken"))
	if offset < 0 || offset > len(list) {
		offset = len(list)
	}
	end := offset + pageSize
	if end > len(list) {
		end = len(list)
	}
	resp := map[string]any{
		"reviews":          list[offset:end],
		"totalReviewCount": len(list),
	}
	if end < len(list) {
		resp["nextPageToken"] = strconv.Itoa(end)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) putReply(w http.ResponseWriter, req *http.Request, loc, reviewID string) {
	var body struct {
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Comment == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "comment required"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.byLoc[loc] {
		if r.ReviewID == reviewID {
			r.Reply = &reviewReply{Comment: body.Comment, UpdateTime: time.Now().UTC()}
			s.log.Info("reply stored", "location", loc, "review_id", reviewID)
			writeJSON(w, http.StatusOK, r.Reply)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "review not found"})
}

// 路徑格式: /v4/accounts/{acc}/locations/{loc}/reviews[...]
func (s *server) route(w http.ResponseWriter, req *http.Request) {
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	if len(parts) < 6 || parts[0] != "v4" || parts[1] != "accounts" || parts[3] != "locations" {
		http.NotFound(w, req)
		return
	}
	loc := "locations/" + parts[4]
	switch {
	case len(parts) == 6 && parts[5] == "reviews" && req.Method == http.MethodGet:
		s.listReviews(w, req, loc)
	case len(parts) == 8 && parts[5] == "reviews" && parts[7] == "reply" && req.Method == http.MethodPut:
		s.putReply(w, req, loc, parts[6])
	default:
		http.NotFound(w, req)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("svc", "mockgoogle")
	port := envutil.Or("PORT", "8081")
	interval, _ := time.ParseDuration(envutil.Or("MOCK_INTERVAL", "20s"))
	seedCount, _ := strconv.Atoi(envutil.Or("MOCK_SEED_COUNT", "8"))
	locations := strings.Split(envutil.Or("MOCK_LOCATIONS", "locations/mock-loc-1,locations/mock-loc-2"), ",")

	s := &server{byLoc: map[string][]*review{}, account: "accounts/mock-account", log: log}
	s.mu.Lock()
	for _, loc := range locations {
		for i := 0; i < seedCount; i++ {
			r := s.addRandomReview(loc)
			// 預埋資料的時間往回推，模擬歷史評論
			back := time.Duration(seedCount-i) * time.Hour
			r.CreateTime = r.CreateTime.Add(-back)
			r.UpdateTime = r.CreateTime
		}
	}
	s.mu.Unlock()

	// 持續產生事件：約 1/3 是編輯既有評論（驗證版本化抓取），其餘為新評論
	go func() {
		for range time.Tick(interval) {
			loc := locations[rand.Intn(len(locations))]
			s.mu.Lock()
			var r *review
			action := "new"
			if rand.Intn(3) == 0 {
				if r = s.editRandomReview(loc); r != nil {
					action = "edited"
				}
			}
			if r == nil {
				r = s.addRandomReview(loc)
			}
			s.mu.Unlock()
			log.Info("review event", "action", action, "location", loc,
				"review_id", r.ReviewID, "star", r.StarRating, "comment", r.Comment)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/v4/", s.route)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	log.Info("mockgoogle listening", "port", port, "locations", locations,
		"interval", interval.String(), "seed_per_location", seedCount)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

