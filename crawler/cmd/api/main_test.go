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
	lastFilter store.CaseFilter
}

func (f *fakeAPIStore) AuthUser(_ context.Context, email, password string) (*store.AuthedUser, error) {
	if f.user != nil && email == f.user.Email && password == "Wachen!2026" {
		return f.user, nil
	}
	return nil, nil
}
func (f *fakeAPIStore) ListCases(_ context.Context, filter store.CaseFilter) ([]store.CaseSummary, error) {
	f.lastFilter = filter
	return f.cases, nil
}
func (f *fakeAPIStore) CaseFacets(_ context.Context) ([]store.Facet, []store.Facet, error) {
	return []store.Facet{{Value: "locations/mock-loc-1", Label: "一號店", Count: 3}},
		[]store.Facet{{Value: "google_review_mock_a", Label: "google_review_mock_a", Count: 3}}, nil
}
func (f *fakeAPIStore) GetPipelineStats(_ context.Context) (*store.PipelineStats, error) {
	p := &store.PipelineStats{}
	p.Funnel.Reviews = 30
	p.Funnel.AwaitingAnalysis = 5
	p.AI.Models = []string{"gemini"}
	p.AI.TotalAnalyses = 25
	return p, nil
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

// 門市/來源篩選：query 參數要完整帶進 store filter
func TestListCasesPassesStoreAndSourceFilters(t *testing.T) {
	st := &fakeAPIStore{user: adminUser()}
	ts := newServer(st)
	defer ts.Close()
	_, out := doLogin(t, ts, "admin@example.com", "Wachen!2026")

	resp := authedReq(t, http.MethodGet,
		ts.URL+"/api/v1/cases?risk=high&status=open&store=locations/mock-loc-1&source=google_review_mock_a&sort=newest",
		out["token"], "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d", resp.StatusCode)
	}
	f := st.lastFilter
	if f.Risk != "high" || f.Status != "open" ||
		f.Store != "locations/mock-loc-1" || f.Source != "google_review_mock_a" || f.Sort != "newest" {
		t.Errorf("filter not fully passed through: %+v", f)
	}
}

func TestFacetsEndpoint(t *testing.T) {
	st := &fakeAPIStore{user: adminUser()}
	ts := newServer(st)
	defer ts.Close()
	_, out := doLogin(t, ts, "admin@example.com", "Wachen!2026")

	resp := authedReq(t, http.MethodGet, ts.URL+"/api/v1/facets", out["token"], "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("facets: %d", resp.StatusCode)
	}
	var body struct {
		Stores  []store.Facet `json:"stores"`
		Sources []store.Facet `json:"sources"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Stores) != 1 || body.Stores[0].Label != "一號店" || body.Stores[0].Count != 3 {
		t.Errorf("stores facet = %+v", body.Stores)
	}
	if len(body.Sources) != 1 {
		t.Errorf("sources facet = %+v", body.Sources)
	}
}

// facets 需要認證
func TestFacetsRequiresAuth(t *testing.T) {
	ts := newServer(&fakeAPIStore{})
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/v1/facets")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("facets without token: %d, want 401", resp.StatusCode)
	}
}

func TestPipelineEndpoint(t *testing.T) {
	ts := newServer(&fakeAPIStore{user: adminUser()})
	defer ts.Close()
	_, out := doLogin(t, ts, "admin@example.com", "Wachen!2026")

	// 需認證
	if resp, _ := http.Get(ts.URL + "/api/v1/pipeline"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("pipeline without token: %d, want 401", resp.StatusCode)
	}
	resp := authedReq(t, http.MethodGet, ts.URL+"/api/v1/pipeline", out["token"], "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pipeline: %d", resp.StatusCode)
	}
	var p store.PipelineStats
	_ = json.NewDecoder(resp.Body).Decode(&p)
	if p.Funnel.Reviews != 30 || p.AI.TotalAnalyses != 25 || len(p.AI.Models) != 1 {
		t.Errorf("pipeline stats = %+v", p)
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
