package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ikala/wachen/crawler/internal/store"
)

type fakeAPIStore struct {
	user       *store.AuthedUser
	cases      []store.CaseSummary
	detail     *store.CaseDetail
	updateErr  error
	lastActor  string
	lastStatus string
}

func (f *fakeAPIStore) AuthUser(_ context.Context, email, password string) (*store.AuthedUser, error) {
	if f.user != nil && email == f.user.Email && password == "Wachen!2026" {
		return f.user, nil
	}
	return nil, nil
}
func (f *fakeAPIStore) ListCases(_ context.Context, _, _ string, _ int) ([]store.CaseSummary, error) {
	return f.cases, nil
}
func (f *fakeAPIStore) GetCaseDetail(_ context.Context, _ string) (*store.CaseDetail, error) {
	return f.detail, nil
}
func (f *fakeAPIStore) UpdateCaseStatus(_ context.Context, _, newStatus, actor string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.lastStatus, f.lastActor = newStatus, actor
	return nil
}

var testLog = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func newServer(st apiStore) *httptest.Server {
	return httptest.NewServer((&server{st: st, log: testLog, secret: []byte("test-secret")}).routes())
}

func adminUser() *store.AuthedUser {
	return &store.AuthedUser{ID: "u1", Email: "admin@example.com", DisplayName: "系統管理員", Role: "admin"}
}

func doLogin(t *testing.T, ts *httptest.Server, email, password string) (*http.Response, map[string]string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	resp, err := http.Post(ts.URL+"/api/v1/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func authedReq(t *testing.T, method, url, token, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestLoginSuccessIssuesToken(t *testing.T) {
	ts := newServer(&fakeAPIStore{user: adminUser()})
	defer ts.Close()

	resp, out := doLogin(t, ts, "admin@example.com", "Wachen!2026")
	if resp.StatusCode != http.StatusOK || out["token"] == "" || out["name"] != "系統管理員" {
		t.Fatalf("status=%d out=%v", resp.StatusCode, out)
	}
}

// 帳號不存在與密碼錯誤同一回應（防枚舉）
func TestLoginFailuresIndistinguishable(t *testing.T) {
	ts := newServer(&fakeAPIStore{user: adminUser()})
	defer ts.Close()

	respWrongPw, outA := doLogin(t, ts, "admin@example.com", "wrong")
	respNoUser, outB := doLogin(t, ts, "ghost@example.com", "wrong")
	if respWrongPw.StatusCode != http.StatusUnauthorized || respNoUser.StatusCode != http.StatusUnauthorized {
		t.Fatalf("statuses = %d/%d, want 401/401", respWrongPw.StatusCode, respNoUser.StatusCode)
	}
	if outA["error"] != outB["error"] {
		t.Error("error messages must be identical (no enumeration)")
	}
}

func TestEndpointsRequireAuth(t *testing.T) {
	ts := newServer(&fakeAPIStore{})
	defer ts.Close()

	for _, path := range []string{"/api/v1/cases", "/api/v1/cases/x"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without token: %d, want 401", path, resp.StatusCode)
		}
	}
	// 偽造簽章的 token 也要拒絕
	resp := authedReq(t, http.MethodGet, ts.URL+"/api/v1/cases",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJoYWNrZXIifQ.forged", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("forged token: %d, want 401", resp.StatusCode)
	}
}

func TestListAndDetailWithToken(t *testing.T) {
	st := &fakeAPIStore{
		user:   adminUser(),
		cases:  []store.CaseSummary{{ID: "c1", RiskLevel: "high", Status: "open"}},
		detail: &store.CaseDetail{CaseSummary: store.CaseSummary{ID: "c1"}, ReviewContent: "難吃", Notifications: []byte("[]")},
	}
	ts := newServer(st)
	defer ts.Close()
	_, out := doLogin(t, ts, "admin@example.com", "Wachen!2026")

	resp := authedReq(t, http.MethodGet, ts.URL+"/api/v1/cases?risk=high", out["token"], "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d", resp.StatusCode)
	}
	var list struct {
		Cases []store.CaseSummary `json:"cases"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if len(list.Cases) != 1 || list.Cases[0].ID != "c1" {
		t.Errorf("cases = %+v", list.Cases)
	}

	resp = authedReq(t, http.MethodGet, ts.URL+"/api/v1/cases/c1", out["token"], "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail: %d", resp.StatusCode)
	}
}

// 狀態變更：稽核 actor 必須是登入使用者（user:<email>），不是服務身分
func TestUpdateStatusCarriesUserActor(t *testing.T) {
	st := &fakeAPIStore{user: adminUser()}
	ts := newServer(st)
	defer ts.Close()
	_, out := doLogin(t, ts, "admin@example.com", "Wachen!2026")

	resp := authedReq(t, http.MethodPatch, ts.URL+"/api/v1/cases/c1/status", out["token"],
		`{"status": "in_progress"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch: %d", resp.StatusCode)
	}
	if st.lastActor != "user:admin@example.com" || st.lastStatus != "in_progress" {
		t.Errorf("actor=%s status=%s", st.lastActor, st.lastStatus)
	}
}

func TestUpdateStatusValidation(t *testing.T) {
	st := &fakeAPIStore{user: adminUser(), updateErr: store.ErrInvalidTransition}
	ts := newServer(st)
	defer ts.Close()
	_, out := doLogin(t, ts, "admin@example.com", "Wachen!2026")

	// 非法字面值 → 400
	if resp := authedReq(t, http.MethodPatch, ts.URL+"/api/v1/cases/c1/status", out["token"],
		`{"status": "deleted"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad literal: %d, want 400", resp.StatusCode)
	}
	// 合法字面值但不可轉換 → 422
	if resp := authedReq(t, http.MethodPatch, ts.URL+"/api/v1/cases/c1/status", out["token"],
		`{"status": "closed"}`); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("invalid transition: %d, want 422", resp.StatusCode)
	}
}
