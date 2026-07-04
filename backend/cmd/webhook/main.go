// Webhook Gateway：推送型來源（官網留言 / APP 留言 / 客服管道 / NPS 匯入）。
// 不用爬——外部系統主動 POST 進來，轉成與爬蟲相同的 raw_reviews + review.raw 事件，
// 之後的 ingestion / 分析 / 分流管線完全共用。
//
//	POST /v1/sources/{source_name}/reviews
//	Header: X-Webhook-Secret: <sources.config.webhook_secret>
//	Body: {
//	  "external_id": "官網系統的留言 ID（必填，冪等鍵）",
//	  "author": "留言者", "rating": 2.0,
//	  "content": "留言內容", "posted_at": "RFC3339",
//	  "source_url": "該則留言在原系統的 permalink（必填）",
//	  "location_id": "對映 stores.google_location_id（選填）"
//	}
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ikala/wachen/backend/internal/adapter"
	"github.com/ikala/wachen/backend/internal/bootstrap"
	"github.com/ikala/wachen/backend/internal/envutil"
	"github.com/ikala/wachen/backend/internal/store"
)

type webhookStore interface {
	EnabledWebhookSource(ctx context.Context, name string) (secret string, found bool, err error)
	InsertRawReviews(ctx context.Context, reviews []adapter.RawReview, jobID string) ([]store.InsertResult, error)
}

type rawPublisher interface {
	PublishReviewRaw(ctx context.Context, sourceName, rawReviewID string) error
}

type inboundReview struct {
	ExternalID string     `json:"external_id"`
	Author     string     `json:"author"`
	Rating     *float64   `json:"rating"`
	Content    string     `json:"content"`
	PostedAt   *time.Time `json:"posted_at"`
	SourceURL  string     `json:"source_url"`
	LocationID string     `json:"location_id"`
}

type handler struct {
	st  webhookStore
	pub rawPublisher
	log *slog.Logger
}

// routes 用 Go 1.22 ServeMux 的 method + wildcard 模式，404/405 交給標準庫
func (h *handler) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sources/{source}/reviews", h.acceptReview)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return mux
}

func (h *handler) acceptReview(w http.ResponseWriter, r *http.Request) {
	sourceName := r.PathValue("source")

	secret, found, err := h.st.EnabledWebhookSource(r.Context(), sourceName)
	if err != nil {
		h.log.Error("source lookup failed", "source", sourceName, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	// 未知來源與密鑰錯誤回應一致（401），不洩漏有效來源名可供枚舉
	if !found || secret == "" ||
		subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Webhook-Secret")), []byte(secret)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// 讀原始 bytes：payload 必須「原文保存」（呼叫方未來新增的欄位不可被 re-marshal 丟棄），
	// hash 也以原文計算
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body too large or unreadable"})
		return
	}
	var in inboundReview
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if in.ExternalID == "" || in.SourceURL == "" || (in.Content == "" && in.Rating == nil) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "external_id, source_url are required; need content or rating"})
		return
	}
	// rating 承載 NPS 0-10；schema CHECK 同步把關（見 migration 000009）
	if in.Rating != nil && (*in.Rating < 0 || *in.Rating > 10) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rating must be within 0-10"})
		return
	}
	// PostgreSQL jsonb 不接受 NUL（原始位元組或 JSON 跳脫 \u0000 都會炸），
	// 擋在門口而非讓 ingest 進毒藥迴圈
	if bytes.ContainsAny(body, "\x00") || bytes.Contains(body, []byte(`\u0000`)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "payload must not contain NUL"})
		return
	}

	sum := sha256.Sum256(body)
	payload := body
	raw := adapter.RawReview{
		SourceName:  sourceName,
		ExternalID:  in.ExternalID,
		Payload:     payload,
		ContentHash: hex.EncodeToString(sum[:]),
		SourceURL:   in.SourceURL,
		LocationID:  in.LocationID,
		FetchedAt:   time.Now().UTC(),
	}
	results, err := h.st.InsertRawReviews(r.Context(), []adapter.RawReview{raw}, "") // 推送型沒有 crawl job
	if err != nil {
		h.log.Error("insert failed", "source", sourceName, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	// 與 worker 同語意：新列與既有版本都發事件；publish 失敗回 5xx 讓對方重送（冪等安全）
	if err := h.pub.PublishReviewRaw(r.Context(), sourceName, results[0].ID); err != nil {
		h.log.Error("publish failed", "raw_review_id", results[0].ID, "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "queue unavailable, retry"})
		return
	}

	status := http.StatusCreated
	if !results[0].Inserted {
		status = http.StatusOK // 冪等重送
	}
	h.log.Info("webhook review accepted", "source", sourceName,
		"external_id", in.ExternalID, "new_version", results[0].Inserted)
	writeJSON(w, status, map[string]any{
		"raw_review_id": results[0].ID,
		"new_version":   results[0].Inserted,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func main() {
	svc := bootstrap.MustInit("webhook", "svc:webhook")
	defer svc.Close()
	ctx, log := svc.Ctx, svc.Log

	h := &handler{st: svc.Store, pub: svc.Queue, log: log}
	mux := h.routes()

	srv := &http.Server{Addr: ":" + envutil.Or("PORT", "8090"), Handler: mux}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	log.Info("webhook gateway listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
	log.Info("shut down")
}
