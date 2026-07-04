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
	GetCaseDetail(ctx context.Context, caseID string) (*store.CaseDetail, error)
	UpdateCaseStatus(ctx context.Context, caseID, newStatus, actor string) error
}

type server struct {
	st     apiStore
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
	mux.Handle("GET /api/v1/cases/{id}", s.auth(s.caseDetail))
	mux.Handle("PATCH /api/v1/cases/{id}/status", s.auth(s.updateStatus))
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
	s := &server{st: svc.Store, log: log, secret: []byte(secret)}

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
