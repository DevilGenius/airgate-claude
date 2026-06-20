package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DevilGenius/airgate-sdk/devkit/devserver"
)

func newTestOAuthHandler(t *testing.T) (*OAuthDevHandler, *http.ServeMux) {
	t.Helper()
	store := devserver.NewAccountStore(filepath.Join(t.TempDir(), "accounts.json"))
	g := &AnthropicGateway{logger: testLogger()}
	h := &OAuthDevHandler{Gateway: g, Store: store}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, mux
}

func TestOAuthDevHandlerRegisterRoutesAndStart(t *testing.T) {
	_, mux := newTestOAuthHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/start", nil)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("start code = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("start JSON error: %v", err)
	}
	if body["authorize_url"] == "" || body["state"] == "" {
		t.Fatalf("start body = %#v", body)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/oauth/start", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET start code = %d", rec.Code)
	}
}

func TestOAuthDevHandlerCallbackRefreshCookieAndBatch(t *testing.T) {
	server := newOAuthFixtureServer(t)
	defer server.Close()
	withOAuthEndpoints(t, server.URL, server.URL+"/oauth/token")

	oldStore := sessionStore
	sessionStore = &oauthSessionStore{sessions: map[string]*OAuthSession{}}
	t.Cleanup(func() { sessionStore = oldStore })
	sessionStore.Set("state", &OAuthSession{State: "state", CodeVerifier: "verifier"})

	_, mux := newTestOAuthHandler(t)

	callbackBody := strings.NewReader(`{"callback_url":"https://platform.claude.com/oauth/code/callback?code=auth-code&state=state"}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/oauth/callback", callbackBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("callback code = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "user@example.test") {
		t.Fatalf("callback body = %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/oauth/refresh", strings.NewReader(`{"refresh_token":"refresh"}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "access-from-refresh") {
		t.Fatalf("refresh code/body = %d/%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/console/cookie-auth", strings.NewReader(`{"session_key":"session-key"}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "user@example.test") {
		t.Fatalf("cookie auth code/body = %d/%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/console/batch-cookie-auth", strings.NewReader(`{"session_keys":["session-key"]}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("batch code/body = %d/%s", rec.Code, rec.Body.String())
	}
}

func TestOAuthDevHandlerValidationErrors(t *testing.T) {
	_, mux := newTestOAuthHandler(t)
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{"callback method", http.MethodGet, "/api/oauth/callback", "", http.StatusMethodNotAllowed},
		{"callback bad json", http.MethodPost, "/api/oauth/callback", "{", http.StatusBadRequest},
		{"callback bad url", http.MethodPost, "/api/oauth/callback", `{"callback_url":"://bad"}`, http.StatusBadRequest},
		{"callback missing code", http.MethodPost, "/api/oauth/callback", `{"callback_url":"https://example.test?state=s"}`, http.StatusBadRequest},
		{"refresh method", http.MethodGet, "/api/oauth/refresh", "", http.StatusMethodNotAllowed},
		{"refresh missing token", http.MethodPost, "/api/oauth/refresh", `{}`, http.StatusBadRequest},
		{"cookie method", http.MethodGet, "/api/console/cookie-auth", "", http.StatusMethodNotAllowed},
		{"cookie missing session", http.MethodPost, "/api/console/cookie-auth", `{}`, http.StatusBadRequest},
		{"batch method", http.MethodGet, "/api/console/batch-cookie-auth", "", http.StatusMethodNotAllowed},
		{"batch missing sessions", http.MethodPost, "/api/console/batch-cookie-auth", `{}`, http.StatusBadRequest},
		{"usage method", http.MethodPost, "/api/accounts/usage/1", "", http.StatusMethodNotAllowed},
		{"usage missing id", http.MethodGet, "/api/accounts/usage/", "", http.StatusBadRequest},
		{"usage invalid id", http.MethodGet, "/api/accounts/usage/not-int", "", http.StatusBadRequest},
		{"usage not found", http.MethodGet, "/api/accounts/usage/99", "", http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if rec.Code != tc.status {
				t.Fatalf("code = %d, want %d body=%s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestOAuthDevHandlerUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":1}}`))
	}))
	defer server.Close()
	withUsageEndpoint(t, server.URL)

	h, mux := newTestOAuthHandler(t)
	noToken := h.Store.Create(devserver.DevAccount{Name: "No Token", AccountType: "oauth", Credentials: map[string]string{}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/accounts/usage/"+strconvFormatInt(noToken.ID), nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no token usage code = %d", rec.Code)
	}

	withToken := h.Store.Create(devserver.DevAccount{Name: "Token", AccountType: "oauth", Credentials: map[string]string{"access_token": "tok"}})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/accounts/usage/"+strconvFormatInt(withToken.ID), nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"five_hour"`) {
		t.Fatalf("usage code/body = %d/%s", rec.Code, rec.Body.String())
	}
}

func strconvFormatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}
