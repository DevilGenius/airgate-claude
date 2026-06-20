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

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
	"github.com/tidwall/gjson"
)

func TestForwardAPIKeySuccessAndError(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var seenPath, seenKey, seenBeta string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seenPath = r.URL.String()
			seenKey = r.Header.Get("x-api-key")
			seenBeta = r.Header.Get("anthropic-beta")
			body, _ := io.ReadAll(r.Body)
			if got := gjson.GetBytes(body, "max_tokens").Int(); got != 4096 {
				t.Fatalf("max_tokens = %d; body=%s", got, body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"claude-opus-4-8","usage":{"input_tokens":1,"output_tokens":2}}`))
		}))
		defer server.Close()

		g := newTestGateway(t)
		rec := httptest.NewRecorder()
		req := &sdk.ForwardRequest{
			Account: &sdk.Account{ID: 11, Type: "apikey", Credentials: map[string]string{"api_key": "sk", "base_url": server.URL}},
			Headers: http.Header{"X-Original-Path": []string{"/v1/messages"}},
			Body:    []byte(`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`),
			Writer:  rec,
			Model:   "claude-opus-4-8",
		}

		outcome, err := g.forwardAPIKey(context.Background(), req, "/v1/messages")
		if err != nil {
			t.Fatalf("forwardAPIKey returned error: %v", err)
		}
		if outcome.Kind != sdk.OutcomeSuccess || rec.Code != http.StatusOK {
			t.Fatalf("outcome/recorder = %#v/%d", outcome, rec.Code)
		}
		if seenPath != "/v1/messages?beta=true" || seenKey != "sk" || !strings.Contains(seenBeta, BetaClaudeCode) {
			t.Fatalf("upstream saw path/key/beta = %q/%q/%q", seenPath, seenKey, seenBeta)
		}
		if outcome.Usage == nil || outcome.Usage.InputTokens != 1 || outcome.Usage.OutputTokens != 2 {
			t.Fatalf("usage was not parsed: %#v", outcome.Usage)
		}
	})

	t.Run("upstream error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"wait"}}`))
		}))
		defer server.Close()

		g := newTestGateway(t)
		req := &sdk.ForwardRequest{
			Account: &sdk.Account{ID: 12, Type: "apikey", Credentials: map[string]string{"api_key": "sk", "base_url": server.URL}},
			Headers: http.Header{},
			Body:    []byte(`{}`),
		}

		outcome, err := g.forwardAPIKey(context.Background(), req, "/v1/messages")
		if err != nil {
			t.Fatalf("forwardAPIKey returned plugin error: %v", err)
		}
		if outcome.Kind != sdk.OutcomeAccountRateLimited || outcome.RetryAfter != 3*timeSecond {
			t.Fatalf("upstream error outcome = %#v", outcome)
		}
	})

	t.Run("bad request URL", func(t *testing.T) {
		g := newTestGateway(t)
		req := &sdk.ForwardRequest{
			Account: &sdk.Account{ID: 13, Type: "apikey", Credentials: map[string]string{"api_key": "sk", "base_url": "http://%zz"}},
			Headers: http.Header{},
			Body:    []byte(`{}`),
		}
		outcome, err := g.forwardAPIKey(context.Background(), req, "/v1/messages")
		if err == nil || outcome.Kind != sdk.OutcomeUpstreamTransient {
			t.Fatalf("bad URL outcome/error = %#v/%v", outcome, err)
		}
	})
}

func TestForwardOAuthSuccessAndClaudeCodeOnlyReject(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var upstreamBody []byte
		var auth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth = r.Header.Get("authorization")
			upstreamBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"claude-haiku-4-5-20251001","usage":{"input_tokens":4,"output_tokens":5}}`))
		}))
		defer server.Close()

		g := newTestGateway(t)
		req := &sdk.ForwardRequest{
			Account: &sdk.Account{
				ID:   21,
				Type: "oauth",
				Credentials: map[string]string{
					"access_token": "tok",
					"base_url":     server.URL,
				},
			},
			Headers: http.Header{},
			Body:    []byte(`{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"hi"}],"temperature":1}`),
			Model:   "claude-haiku-4-5",
		}

		outcome, err := g.forwardOAuth(context.Background(), req, "/v1/messages")
		if err != nil {
			t.Fatalf("forwardOAuth returned error: %v", err)
		}
		if outcome.Kind != sdk.OutcomeSuccess || outcome.Usage == nil || outcome.Usage.InputTokens != 4 {
			t.Fatalf("forwardOAuth outcome = %#v", outcome)
		}
		if auth != "Bearer tok" {
			t.Fatalf("authorization = %q", auth)
		}
		if !strings.Contains(gjson.GetBytes(upstreamBody, "system.0.text").String(), "Claude Code") {
			t.Fatalf("OAuth body was not rewritten: %s", upstreamBody)
		}
		if gjson.GetBytes(upstreamBody, "temperature").Exists() {
			t.Fatalf("temperature was not stripped: %s", upstreamBody)
		}
	})

	t.Run("claude code only reject", func(t *testing.T) {
		g := newTestGateway(t)
		req := &sdk.ForwardRequest{
			Account: &sdk.Account{
				ID:          22,
				Type:        "oauth",
				Credentials: map[string]string{"access_token": "tok", "claude_code_only": "true"},
			},
			Headers: http.Header{"User-Agent": []string{"python-requests/2.31"}},
			Body:    []byte(`{}`),
			Model:   "claude-opus-4-8",
		}

		outcome, err := g.forwardOAuth(context.Background(), req, "/v1/messages")
		if err != nil {
			t.Fatalf("reject should be a client outcome, got error: %v", err)
		}
		if outcome.Kind != sdk.OutcomeClientError || outcome.Upstream.StatusCode != http.StatusForbidden {
			t.Fatalf("reject outcome = %#v", outcome)
		}
	})
}

func TestForwardCountTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "/v1/messages/count_tokens?beta=true" {
			t.Fatalf("upstream path = %q", r.URL.String())
		}
		if got := r.Header.Get("anthropic-beta"); !strings.Contains(got, BetaTokenCounting) {
			t.Fatalf("count tokens beta = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"input_tokens":123}`))
	}))
	defer server.Close()

	g := newTestGateway(t)
	rec := httptest.NewRecorder()
	outcome, err := g.forwardCountTokens(context.Background(), &sdk.ForwardRequest{
		Account: &sdk.Account{ID: 31, Type: "apikey", Credentials: map[string]string{"api_key": "sk", "base_url": server.URL}},
		Body:    []byte(`{"model":"claude-opus-4-8"}`),
		Writer:  rec,
	})
	if err != nil {
		t.Fatalf("forwardCountTokens returned error: %v", err)
	}
	if outcome.Kind != sdk.OutcomeSuccess || outcome.Upstream.StatusCode != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"input_tokens":123}` {
		t.Fatalf("count tokens outcome/body = %#v/%q", outcome, rec.Body.String())
	}
}

func TestForwardHTTPDispatch(t *testing.T) {
	g := newTestGateway(t)

	outcome, err := g.forwardHTTP(context.Background(), &sdk.ForwardRequest{
		Headers: http.Header{"X-Original-Path": []string{"/v1/models"}},
	})
	if err != nil || outcome.Kind != sdk.OutcomeSuccess {
		t.Fatalf("models dispatch outcome/error = %#v/%v", outcome, err)
	}

	outcome, err = g.forwardHTTP(context.Background(), &sdk.ForwardRequest{
		Account: &sdk.Account{ID: 41, Type: "bad", Credentials: map[string]string{}},
		Headers: http.Header{},
		Body:    []byte(`{}`),
	})
	if err == nil || outcome.Kind != sdk.OutcomeAccountDead {
		t.Fatalf("unknown dispatch outcome/error = %#v/%v", outcome, err)
	}
}

func TestValidateAccount(t *testing.T) {
	g := newTestGateway(t)
	if err := g.ValidateAccount(context.Background(), map[string]string{}); err == nil {
		t.Fatalf("missing credentials should fail")
	}
	if err := g.ValidateAccount(context.Background(), map[string]string{"access_token": "tok"}); err != nil {
		t.Fatalf("access token validation should be local: %v", err)
	}
	if err := g.ValidateAccount(context.Background(), map[string]string{"session_key": "sk"}); err != nil {
		t.Fatalf("session key validation should be local: %v", err)
	}

	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{"ok", 200, false},
		{"unauthorized", 401, true},
		{"server error", 500, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/models" {
					t.Fatalf("validate path = %q", r.URL.Path)
				}
				if r.Header.Get("x-api-key") != "sk" {
					t.Fatalf("x-api-key = %q", r.Header.Get("x-api-key"))
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()

			err := g.ValidateAccount(context.Background(), map[string]string{"api_key": "sk", "base_url": server.URL})
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateAccount error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestHandleRequestValidationBranches(t *testing.T) {
	g := &AnthropicGateway{logger: testLogger()}

	cases := []struct {
		name   string
		path   string
		body   []byte
		status int
	}{
		{"unknown", "missing", nil, http.StatusNotFound},
		{"oauth exchange invalid json", "oauth/exchange", []byte(`{`), http.StatusBadRequest},
		{"oauth exchange invalid callback", "oauth/exchange", []byte(`{"callback_url":"://bad"}`), http.StatusBadRequest},
		{"oauth exchange missing code", "oauth/exchange", []byte(`{"callback_url":"https://platform.claude.com/oauth/code/callback?state=s"}`), http.StatusBadRequest},
		{"oauth refresh missing token", "oauth/refresh", []byte(`{}`), http.StatusBadRequest},
		{"usage accounts invalid body", "usage/accounts", []byte(`{}`), http.StatusBadRequest},
		{"cookie auth missing session", "console/cookie-auth", []byte(`{}`), http.StatusBadRequest},
		{"batch cookie auth missing sessions", "console/batch-cookie-auth", []byte(`{}`), http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, _, _, err := g.HandleRequest(context.Background(), "", tc.path, "", nil, tc.body)
			if err != nil {
				t.Fatalf("HandleRequest returned plugin error: %v", err)
			}
			if status != tc.status {
				t.Fatalf("status = %d, want %d", status, tc.status)
			}
		})
	}

	status, _, body, err := g.HandleRequest(context.Background(), "", "usage/accounts", "", nil, []byte(`[{"id":1,"credentials":{}}]`))
	if err != nil || status != http.StatusOK {
		t.Fatalf("usage/accounts empty-token status/error = %d/%v", status, err)
	}
	var usageResp accountUsageAccountsResponse
	if err := json.Unmarshal(body, &usageResp); err != nil {
		t.Fatalf("usage response JSON error: %v", err)
	}
	if len(usageResp.Accounts) != 0 || len(usageResp.Errors) != 0 {
		t.Fatalf("empty-token usage response = %#v", usageResp)
	}

	status, _, body, err = g.HandleRequest(context.Background(), "", "oauth/start", "", nil, nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("oauth/start status/error = %d/%v body=%s", status, err, body)
	}
	if !gjson.GetBytes(body, "authorize_url").Exists() || !gjson.GetBytes(body, "state").Exists() {
		t.Fatalf("oauth/start body = %s", body)
	}
}

const timeSecond = time.Second
