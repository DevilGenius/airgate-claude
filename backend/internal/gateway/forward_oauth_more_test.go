package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestForwardOAuthRefreshesTokenAndReturnsUpdatedCredentials(t *testing.T) {
	tokenServer := newOAuthFixtureServer(t)
	defer tokenServer.Close()
	withOAuthEndpoints(t, tokenServer.URL, tokenServer.URL+"/oauth/token")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer access-from-refresh" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"model":"claude-opus-4-8","usage":{"input_tokens":2,"output_tokens":3}}`))
	}))
	defer upstream.Close()

	g := newTestGateway(t)
	account := &sdk.Account{ID: 61, Type: "oauth", Credentials: map[string]string{
		"access_token":  "expired",
		"refresh_token": "refresh",
		"expires_at":    time.Now().Add(-time.Hour).Format(time.RFC3339),
		"base_url":      upstream.URL,
	}}

	outcome, err := g.forwardOAuth(context.Background(), &sdk.ForwardRequest{
		Account: account,
		Headers: http.Header{},
		Body:    []byte(`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`),
		Model:   "claude-opus-4-8",
	}, "/v1/messages")
	if err != nil {
		t.Fatalf("forwardOAuth returned error: %v", err)
	}
	if outcome.Kind != sdk.OutcomeSuccess || outcome.UpdatedCredentials["access_token"] != "access-from-refresh" {
		t.Fatalf("outcome = %#v", outcome)
	}
	if account.Credentials["refresh_token"] != "refresh-next" {
		t.Fatalf("account credentials were not refreshed: %#v", account.Credentials)
	}
}

func TestForwardOAuthTokenErrorsAndMissingAccessToken(t *testing.T) {
	g := newTestGateway(t)

	missingAccess := &sdk.ForwardRequest{
		Account: &sdk.Account{ID: 62, Type: "oauth", Credentials: map[string]string{}},
		Headers: http.Header{},
		Body:    []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}
	outcome, err := g.forwardOAuth(context.Background(), missingAccess, "/v1/messages")
	if err == nil || outcome.Kind != sdk.OutcomeAccountDead {
		t.Fatalf("missing access outcome/error = %#v/%v", outcome, err)
	}

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_grant","message":"revoked"}}`))
	}))
	defer tokenServer.Close()
	withOAuthEndpoints(t, tokenServer.URL, tokenServer.URL)

	refreshFailure := &sdk.ForwardRequest{
		Account: &sdk.Account{ID: 63, Type: "oauth", Credentials: map[string]string{
			"access_token":  "expired",
			"refresh_token": "refresh",
			"expires_at":    time.Now().Add(-time.Hour).Format(time.RFC3339),
		}},
		Headers: http.Header{},
		Body:    []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}
	outcome, err = g.forwardOAuth(context.Background(), refreshFailure, "/v1/messages")
	if err == nil || outcome.Kind != sdk.OutcomeAccountDead || !strings.Contains(outcome.Reason, "token 刷新失败") {
		t.Fatalf("refresh failure outcome/error = %#v/%v", outcome, err)
	}
}

func TestForwardCountTokensErrors(t *testing.T) {
	t.Run("upstream error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"bad key"}}`))
		}))
		defer server.Close()

		g := newTestGateway(t)
		outcome, err := g.forwardCountTokens(context.Background(), &sdk.ForwardRequest{
			Account: &sdk.Account{ID: 64, Type: "apikey", Credentials: map[string]string{"api_key": "bad", "base_url": server.URL}},
			Body:    []byte(`{}`),
		})
		if err != nil || outcome.Kind != sdk.OutcomeAccountDead || !strings.Contains(outcome.Reason, "authentication_error") {
			t.Fatalf("count_tokens error outcome/error = %#v/%v", outcome, err)
		}
	})

	t.Run("bad request URL", func(t *testing.T) {
		g := newTestGateway(t)
		outcome, err := g.forwardCountTokens(context.Background(), &sdk.ForwardRequest{
			Account: &sdk.Account{ID: 65, Type: "apikey", Credentials: map[string]string{"api_key": "sk", "base_url": "http://%zz"}},
			Body:    []byte(`{}`),
		})
		if err == nil || outcome.Kind != sdk.OutcomeUpstreamTransient {
			t.Fatalf("bad URL outcome/error = %#v/%v", outcome, err)
		}
	})
}
