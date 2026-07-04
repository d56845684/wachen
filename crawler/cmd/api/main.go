// 後台 REST API（M6）：帳密登入（JWT）+ 案件收件匣/詳情/狀態變更。
// PoC 範圍：僅認證、無 RBAC（角色欄位帶在 token 裡，M-later 再啟用授權）。
//
//	POST  /api/v1/login              {email, password} → {token, name}
//	GET   /api/v1/cases?risk=&status=                  （Bearer）
//	GET   /api/v1/cases/{id}                           （Bearer）
//	PATCH /api/v1/cases/{id}/status  {status}          （Bearer；稽核 actor = user:<email>）
//
// 對外只經 nginx（/api/ 反向代理），本服務不曝露 port。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ikala/wachen/crawler/internal/bootstrap"
	"github.com/ikala/wachen/crawler/internal/envutil"
	"github.com/ikala/wachen/crawler/internal/store"
)

type apiStore interface {
	AuthUser(ctx context.Context, email, password string) (*store.AuthedUser, error)
	ListCases(ctx context.Context, f store.CaseFilter) ([]store.CaseSummary, error)
	CaseFacets(ctx context.Context) (stores, sources []store.Facet, err error)
	GetPipelineStats(ctx context.Context) (*store.PipelineStats, error)
	GetCaseDetail(ctx context.Context, caseID string) (*store.CaseDetail, error)
	UpdateCaseStatus(ctx context.Context, caseID, newStatus, actor string) error
	CreateReply(ctx context.Context, caseID, content, authorEmail string) (*store.Reply, bool, error)
	ApproveReply(ctx context.Context, replyID, approverEmail string) (bool, error)
	RejectReply(ctx context.Context, replyID, approverEmail, reason string) error
	PendingApprovals(ctx context.Context, limit int) ([]store.PendingReply, error)
}

// enqueuer：建立/核准回覆後把 reply.requested 推進佇列（實作為 *queue.Queue）
type enqueuer interface {
	PublishReplyRequested(ctx context.Context, replyID string) error
}

type server struct {
	st     apiStore
	q      enqueuer
	log    *slog.Logger
	secret []byte
}

type ctxKey string

const userKey ctxKey = "user"

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/login", s.login)
	mux.Handle("GET /api/v1/cases", s.auth(s.listCases))
	mux.Handle("GET /api/v1/facets", s.auth(s.facets))
	mux.Handle("GET /api/v1/pipeline", s.auth(s.pipeline))
	mux.Handle("GET /api/v1/cases/{id}", s.auth(s.caseDetail))
	mux.Handle("PATCH /api/v1/cases/{id}/status", s.auth(s.updateStatus))
	mux.Handle("POST /api/v1/cases/{id}/replies", s.auth(s.createReply))
	mux.Handle("GET /api/v1/approvals", s.auth(s.approvals))
	mux.Handle("POST /api/v1/replies/{id}/approve", s.auth(s.approveReply))
	mux.Handle("POST /api/v1/replies/{id}/reject", s.auth(s.rejectReply))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return mux
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil ||
		in.Email == "" || in.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password required"})
		return
	}
	u, err := s.st.AuthUser(r.Context(), in.Email, in.Password)
	if err != nil {
		s.log.Error("auth query failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	if u == nil {
		// 帳號不存在與密碼錯誤同一回應（防枚舉）
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "帳號或密碼錯誤"})
		return
	}
	claims := jwt.MapClaims{
		"sub":  u.Email,
		"name": u.DisplayName,
		"role": u.Role, // 帶著但 PoC 不做授權檢查
		"exp":  time.Now().Add(12 * time.Hour).Unix(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	s.log.Info("login ok", "user", u.Email)
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "name": u.DisplayName})
}

// auth：驗 Bearer token，把使用者 email 放進 context（稽核 actor 用）
func (s *server) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if raw == "" || raw == r.Header.Get("Authorization") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
			return
		}
		tok, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return s.secret, nil
		})
		if err != nil || !tok.Valid {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			return
		}
		sub, _ := tok.Claims.GetSubject()
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, sub)))
	})
}

func (s *server) listCases(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cases, err := s.st.ListCases(r.Context(), store.CaseFilter{
		Risk:   q.Get("risk"),
		Status: q.Get("status"),
		Store:  q.Get("store"),
		Source: q.Get("source"),
		Rating: q.Get("rating"),
		Sort:   q.Get("sort"),
		Limit:  200,
	})
	if err != nil {
		s.log.Error("list cases failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	if cases == nil {
		cases = []store.CaseSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"cases": cases})
}

func (s *server) facets(w http.ResponseWriter, r *http.Request) {
	stores, sources, err := s.st.CaseFacets(r.Context())
	if err != nil {
		s.log.Error("facets failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stores": stores, "sources": sources})
}

func (s *server) pipeline(w http.ResponseWriter, r *http.Request) {
	stats, err := s.st.GetPipelineStats(r.Context())
	if err != nil {
		s.log.Error("pipeline stats failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *server) caseDetail(w http.ResponseWriter, r *http.Request) {
	d, err := s.st.GetCaseDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		s.log.Error("case detail failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	if d == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "case not found"})
		return
	}
	writeJSON(w, http.StatusOK, d)
}

var allowedStatuses = map[string]bool{"open": true, "in_progress": true, "resolved": true, "closed": true}

func (s *server) updateStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil ||
		!allowedStatuses[in.Status] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be one of open/in_progress/resolved/closed"})
		return
	}
	email, _ := r.Context().Value(userKey).(string)
	err := s.st.UpdateCaseStatus(r.Context(), r.PathValue("id"), in.Status, "user:"+email)
	switch {
	case errors.Is(err, store.ErrInvalidTransition):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "此狀態不可轉換"})
		return
	case err != nil:
		s.log.Error("update status failed", "case", r.PathValue("id"), "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	s.log.Info("case status updated", "case", r.PathValue("id"), "status", in.Status, "by", email)
	writeJSON(w, http.StatusOK, map[string]string{"status": in.Status})
}

func (s *server) createReply(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil ||
		len(in.Content) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content required"})
		return
	}
	email, _ := r.Context().Value(userKey).(string)
	reply, enqueue, err := s.st.CreateReply(r.Context(), r.PathValue("id"), in.Content, email)
	switch {
	case errors.Is(err, store.ErrReplyNotAllowed):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "此來源不支援回覆"})
		return
	case errors.Is(err, store.ErrReplyTooLong):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "回覆內容過長"})
		return
	case errors.Is(err, store.ErrCaseNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "case not found"})
		return
	case err != nil:
		s.log.Error("create reply failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	if enqueue {
		if err := s.q.PublishReplyRequested(r.Context(), reply.ID); err != nil {
			// 已寫入 DB（approved），發送失敗不擋使用者；worker 另有 approved 掃描補送（M-later）
			s.log.Error("enqueue reply failed", "reply_id", reply.ID, "err", err)
		}
	}
	s.log.Info("reply created", "reply_id", reply.ID, "status", reply.Status, "by", email)
	writeJSON(w, http.StatusCreated, reply)
}

func (s *server) approvals(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.PendingApprovals(r.Context(), 100)
	if err != nil {
		s.log.Error("approvals failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"replies": list})
}

func (s *server) approveReply(w http.ResponseWriter, r *http.Request) {
	email, _ := r.Context().Value(userKey).(string)
	ok, err := s.st.ApproveReply(r.Context(), r.PathValue("id"), email)
	if errors.Is(err, store.ErrReplyBadState) || (err == nil && !ok) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "此回覆不在待審狀態"})
		return
	}
	if err != nil {
		s.log.Error("approve reply failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	if err := s.q.PublishReplyRequested(r.Context(), r.PathValue("id")); err != nil {
		s.log.Error("enqueue reply failed", "reply_id", r.PathValue("id"), "err", err)
	}
	s.log.Info("reply approved", "reply_id", r.PathValue("id"), "by", email)
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (s *server) rejectReply(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&in)
	email, _ := r.Context().Value(userKey).(string)
	err := s.st.RejectReply(r.Context(), r.PathValue("id"), email, in.Reason)
	if errors.Is(err, store.ErrReplyBadState) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "此回覆不在待審狀態"})
		return
	}
	if err != nil {
		s.log.Error("reject reply failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	s.log.Info("reply rejected", "reply_id", r.PathValue("id"), "by", email)
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func main() {
	svc := bootstrap.MustInit("api", "svc:api")
	defer svc.Close()
	ctx, log := svc.Ctx, svc.Log

	secret := envutil.Or("JWT_SECRET", "dev_jwt_secret_change_me")
	if secret == "dev_jwt_secret_change_me" {
		log.Warn("JWT_SECRET not set: using dev default (fine for PoC, not for prod)")
	}
	s := &server{st: svc.Store, q: svc.Queue, log: log, secret: []byte(secret)}

	srv := &http.Server{Addr: ":" + envutil.Or("PORT", "8070"), Handler: s.routes()}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	log.Info("api listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
	log.Info("shut down")
}
