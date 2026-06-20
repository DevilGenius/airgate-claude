package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/imroc/req/v3"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func withOAuthEndpoints(t *testing.T, claudeBase, tokenURL string) {
	t.Helper()
	oldClaudeBase := claudeAIBaseEndpoint
	oldToken := oauthTokenEndpoint
	oldAuthorize := oauthAuthorizeEndpoint
	claudeAIBaseEndpoint = claudeBase
	oauthTokenEndpoint = tokenURL
	oauthAuthorizeEndpoint = claudeBase + "/oauth/authorize"
	t.Cleanup(func() {
		claudeAIBaseEndpoint = oldClaudeBase
		oauthTokenEndpoint = oldToken
		oauthAuthorizeEndpoint = oldAuthorize
	})
}

func newOAuthFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/organizations":
			if cookie, err := r.Cookie("sessionKey"); err != nil || cookie.Value == "" {
				t.Fatalf("missing sessionKey cookie: %v", err)
			}
			_, _ = w.Write([]byte(`[{"uuid":"personal","name":"Personal"},{"uuid":"team","name":"Team","raven_type":"team"}]`))

		case r.Method == http.MethodPost && r.URL.Path == "/v1/oauth/team/authorize":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("authorize request JSON error: %v", err)
			}
			if body["client_id"] != OAuthClientID || body["organization_uuid"] != "team" {
				t.Fatalf("authorize body = %#v", body)
			}
			_, _ = w.Write([]byte(`{"redirect_uri":"https://platform.claude.com/oauth/code/callback?code=auth-code&state=returned-state"}`))

		case r.Method == http.MethodPost && r.URL.Path == "/oauth/token":
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("token request JSON error: %v", err)
			}
			switch body["grant_type"] {
			case "authorization_code":
				_, _ = w.Write([]byte(`{"access_token":"access-from-code","refresh_token":"refresh-from-code","expires_in":3600,"account":{"uuid":"acct","email_address":"user@example.test"},"organization":{"uuid":"org"}}`))
			case "refresh_token":
				_, _ = w.Write([]byte(`{"access_token":"access-from-refresh","refresh_token":"refresh-next","expires_in":7200}`))
			default:
				t.Fatalf("unexpected token grant body: %#v", body)
			}

		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	return server
}

func TestExchangeSessionKeyWithScopeSuccess(t *testing.T) {
	server := newOAuthFixtureServer(t)
	defer server.Close()
	withOAuthEndpoints(t, server.URL, server.URL+"/oauth/token")

	g := &AnthropicGateway{logger: testLogger()}
	token, err := g.exchangeSessionKeyWithScope(context.Background(), "session-key", "", OAuthScopeAPI)
	if err != nil {
		t.Fatalf("exchangeSessionKeyWithScope returned error: %v", err)
	}
	if token.AccessToken != "access-from-code" || token.RefreshToken != "refresh-from-code" || token.Account.EmailAddress != "user@example.test" {
		t.Fatalf("token = %#v", token)
	}
}

func TestGetOrganizationUUIDErrorsAndPreference(t *testing.T) {
	t.Run("team preference", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`[{"uuid":"personal"},{"uuid":"team","raven_type":"team"}]`))
		}))
		defer server.Close()
		withOAuthEndpoints(t, server.URL, server.URL+"/oauth/token")

		g := &AnthropicGateway{}
		got, err := g.getOrganizationUUID(context.Background(), req.C(), "session")
		if err != nil || got != "team" {
			t.Fatalf("org UUID/error = %q/%v", got, err)
		}
	})

	t.Run("http error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`forbidden`))
		}))
		defer server.Close()
		withOAuthEndpoints(t, server.URL, server.URL+"/oauth/token")

		g := &AnthropicGateway{}
		if _, err := g.getOrganizationUUID(context.Background(), req.C(), "session"); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
			t.Fatalf("HTTP error = %v", err)
		}
	})

	t.Run("empty orgs", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`[]`))
		}))
		defer server.Close()
		withOAuthEndpoints(t, server.URL, server.URL+"/oauth/token")

		g := &AnthropicGateway{}
		if _, err := g.getOrganizationUUID(context.Background(), req.C(), "session"); err == nil || !strings.Contains(err.Error(), "未找到组织") {
			t.Fatalf("empty orgs error = %v", err)
		}
	})
}

func TestGetAuthorizationCodeWithScopeErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
		want string
	}{
		{"http error", `forbidden`, http.StatusForbidden, "HTTP 403"},
		{"missing redirect", `{}`, http.StatusOK, "缺少 redirect_uri"},
		{"bad redirect", `{"redirect_uri":"://bad"}`, http.StatusOK, "解析 redirect_uri 失败"},
		{"missing code", `{"redirect_uri":"https://platform.claude.com/oauth/code/callback?state=s"}`, http.StatusOK, "缺少 code"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			withOAuthEndpoints(t, server.URL, server.URL+"/oauth/token")

			g := &AnthropicGateway{}
			_, err := g.getAuthorizationCodeWithScope(context.Background(), req.C(), "session", "org", "challenge", "state", OAuthScopeAPI)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want contains %q", err, tc.want)
			}
		})
	}
}

func TestRefreshTokenWrappers(t *testing.T) {
	server := newOAuthFixtureServer(t)
	defer server.Close()
	withOAuthEndpoints(t, server.URL, server.URL+"/oauth/token")

	g := &AnthropicGateway{logger: testLogger()}
	token, err := g.RefreshToken(context.Background(), "refresh", "")
	if err != nil {
		t.Fatalf("RefreshToken returned error: %v", err)
	}
	if token.AccessToken != "access-from-refresh" || token.RefreshToken != "refresh-next" {
		t.Fatalf("refresh token = %#v", token)
	}

	token, err = g.RefreshTokenForAccount(context.Background(), 12, "refresh", "", "bun-2.1.112")
	if err != nil {
		t.Fatalf("RefreshTokenForAccount returned error: %v", err)
	}
	if token.AccessToken != "access-from-refresh" {
		t.Fatalf("refresh for account token = %#v", token)
	}

	client := g.buildOAuthClient(12, "", "")
	if client == nil || client.Transport == nil {
		t.Fatalf("buildOAuthClient returned %#v", client)
	}
}

func TestTokenManagerRefreshAndSessionExchangeSuccess(t *testing.T) {
	server := newOAuthFixtureServer(t)
	defer server.Close()
	withOAuthEndpoints(t, server.URL, server.URL+"/oauth/token")

	g := &AnthropicGateway{logger: testLogger()}
	m := newTokenManager(g, testLogger())

	expired := &sdk.Account{ID: 50, Type: "oauth", Credentials: map[string]string{
		"access_token":  "old",
		"refresh_token": "refresh",
		"expires_at":    time.Now().Add(-time.Hour).Format(time.RFC3339),
	}}
	updated, err := m.ensureValidToken(context.Background(), expired)
	if err != nil {
		t.Fatalf("ensureValidToken refresh returned error: %v", err)
	}
	if updated["access_token"] != "access-from-refresh" || expired.Credentials["refresh_token"] != "refresh-next" {
		t.Fatalf("refresh update = %#v account=%#v", updated, expired.Credentials)
	}

	sessionAccount := &sdk.Account{ID: 51, Type: "session_key", Credentials: map[string]string{"session_key": "session-key"}}
	updated, err = m.ensureValidToken(context.Background(), sessionAccount)
	if err != nil {
		t.Fatalf("ensureValidToken session exchange returned error: %v", err)
	}
	if updated["access_token"] != "access-from-code" || sessionAccount.Credentials["refresh_token"] != "refresh-from-code" {
		t.Fatalf("session exchange update = %#v account=%#v", updated, sessionAccount.Credentials)
	}
}
