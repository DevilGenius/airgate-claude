package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuthSessionStore(t *testing.T) {
	store := &oauthSessionStore{sessions: map[string]*OAuthSession{}}
	now := time.Now()

	store.Set("fresh", &OAuthSession{State: "fresh", CodeVerifier: "verifier", CreatedAt: now})
	if got, ok := store.Get("fresh"); !ok || got.CodeVerifier != "verifier" {
		t.Fatalf("Get fresh = %#v/%v", got, ok)
	}

	store.Delete("fresh")
	if _, ok := store.Get("fresh"); ok {
		t.Fatalf("deleted session was returned")
	}

	store.Set("zero-created-at", &OAuthSession{State: "zero"})
	if got, ok := store.Get("zero-created-at"); !ok || got.CreatedAt.IsZero() {
		t.Fatalf("Set should fill CreatedAt: %#v/%v", got, ok)
	}

	store.Set("expired", &OAuthSession{State: "expired", CreatedAt: now.Add(-oauthSessionTTL - time.Second)})
	if _, ok := store.Get("expired"); ok {
		t.Fatalf("expired session should not be returned")
	}

	store.sessions = map[string]*OAuthSession{
		"old": {State: "old", CreatedAt: now.Add(-2 * time.Hour)},
		"new": {State: "new", CreatedAt: now},
	}
	store.CleanExpired()
	if _, ok := store.sessions["old"]; ok {
		t.Fatalf("CleanExpired left expired session")
	}
	if _, ok := store.sessions["new"]; !ok {
		t.Fatalf("CleanExpired removed fresh session")
	}

	store.deleteOldestLocked()
	if len(store.sessions) != 0 {
		t.Fatalf("deleteOldestLocked left sessions: %#v", store.sessions)
	}
}

func TestStartOAuthBuildsAuthorizeURLAndStoresSession(t *testing.T) {
	oldStore := sessionStore
	sessionStore = &oauthSessionStore{sessions: map[string]*OAuthSession{}}
	t.Cleanup(func() { sessionStore = oldStore })

	g := &AnthropicGateway{logger: testLogger()}
	resp, err := g.StartOAuth()
	if err != nil {
		t.Fatalf("StartOAuth returned error: %v", err)
	}
	if resp.State == "" || resp.AuthorizeURL == "" {
		t.Fatalf("empty StartOAuth response: %#v", resp)
	}
	if _, ok := sessionStore.Get(resp.State); !ok {
		t.Fatalf("StartOAuth did not store session for state %q", resp.State)
	}

	parsed, err := url.Parse(resp.AuthorizeURL)
	if err != nil {
		t.Fatalf("authorize URL parse failed: %v", err)
	}
	q := parsed.Query()
	if parsed.String() == "" || q.Get("client_id") != OAuthClientID || q.Get("state") != resp.State || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorize URL query = %s", parsed.RawQuery)
	}
	if q.Get("scope") != OAuthScopeBrowser {
		t.Fatalf("scope = %q", q.Get("scope"))
	}
}

func TestHandleOAuthCallbackInvalidState(t *testing.T) {
	oldStore := sessionStore
	sessionStore = &oauthSessionStore{sessions: map[string]*OAuthSession{}}
	t.Cleanup(func() { sessionStore = oldStore })

	g := &AnthropicGateway{logger: testLogger()}
	if _, err := g.HandleOAuthCallback(context.Background(), "code", "missing", ""); err == nil {
		t.Fatalf("HandleOAuthCallback with missing state should fail")
	}
}

func TestExchangeSessionKeyRejectsProxyURL(t *testing.T) {
	g := &AnthropicGateway{}
	_, err := g.ExchangeSessionKeyForToken(context.Background(), "session", "http://proxy.example")
	if err == nil || !strings.Contains(err.Error(), "does not support proxy_url") {
		t.Fatalf("ExchangeSessionKeyForToken proxy error = %v", err)
	}
}

func TestExchangeCodeForToken(t *testing.T) {
	var seenBody map[string]any
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != OAuthTokenURL {
			t.Fatalf("token URL = %s", req.URL)
		}
		if req.Header.Get("Content-Type") != "application/json" || req.Header.Get("User-Agent") != "axios/1.8.4" {
			t.Fatalf("headers = %#v", req.Header)
		}
		body, _ := io.ReadAll(req.Body)
		if err := json.Unmarshal(body, &seenBody); err != nil {
			t.Fatalf("request body JSON error: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"access","refresh_token":"refresh","expires_in":3600,"account":{"uuid":"acct","email_address":"a@example.test"},"organization":{"uuid":"org"}}`)),
			Request:    req,
		}, nil
	})}
	g := &AnthropicGateway{}

	token, err := g.exchangeCodeForToken(context.Background(), client, "auth-code#callback-state", "verifier", "ignored-state")
	if err != nil {
		t.Fatalf("exchangeCodeForToken returned error: %v", err)
	}
	if token.AccessToken != "access" || token.RefreshToken != "refresh" || token.Account.EmailAddress != "a@example.test" || token.Organization.UUID != "org" {
		t.Fatalf("token response = %#v", token)
	}
	if seenBody["code"] != "auth-code" || seenBody["state"] != "callback-state" || seenBody["code_verifier"] != "verifier" {
		t.Fatalf("request body = %#v", seenBody)
	}
}

func TestExchangeCodeForTokenErrors(t *testing.T) {
	g := &AnthropicGateway{}

	httpErrorClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(`{"error":"invalid"}`)), Request: req}, nil
	})}
	if _, err := g.exchangeCodeForToken(context.Background(), httpErrorClient, "code", "verifier", "state"); err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("HTTP error = %v", err)
	}

	decodeErrorClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{`)), Request: req}, nil
	})}
	if _, err := g.exchangeCodeForToken(context.Background(), decodeErrorClient, "code", "verifier", "state"); err == nil || !strings.Contains(err.Error(), "解析 token 响应失败") {
		t.Fatalf("decode error = %v", err)
	}
}

func TestPKCEHelpers(t *testing.T) {
	state, err := generateState()
	if err != nil {
		t.Fatalf("generateState returned error: %v", err)
	}
	if strings.ContainsAny(state, "+/=") || len(state) == 0 {
		t.Fatalf("state is not unpadded URL-safe base64: %q", state)
	}

	verifier, err := generateCodeVerifier()
	if err != nil {
		t.Fatalf("generateCodeVerifier returned error: %v", err)
	}
	if strings.ContainsAny(verifier, "+/=") || len(verifier) == 0 {
		t.Fatalf("verifier is not unpadded URL-safe base64: %q", verifier)
	}

	challenge := generateCodeChallenge("verifier")
	if challenge == "" || strings.ContainsAny(challenge, "+/=") {
		t.Fatalf("challenge = %q", challenge)
	}

	if got := base64URLEncode([]byte{0xff, 0xee}); strings.Contains(got, "=") {
		t.Fatalf("base64URLEncode should strip padding: %q", got)
	}
}
