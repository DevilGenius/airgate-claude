package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
	"github.com/tidwall/gjson"
)

func TestResolveRequestPathAndBaseURL(t *testing.T) {
	if got := resolveRequestPath(&sdk.ForwardRequest{Headers: http.Header{"X-Original-Path": []string{"/v1/messages/count_tokens"}}}); got != "/v1/messages/count_tokens" {
		t.Fatalf("path from header = %q", got)
	}
	if got := resolveRequestPath(&sdk.ForwardRequest{Headers: http.Header{}, Body: []byte(`{}`)}); got != "/v1/messages" {
		t.Fatalf("path from body = %q", got)
	}
	if got := resolveRequestPath(&sdk.ForwardRequest{Headers: http.Header{}}); got != "/v1/models" {
		t.Fatalf("path fallback = %q", got)
	}

	if got := resolveBaseURL(map[string]string{"base_url": " https://example.test/// "}); got != "https://example.test" {
		t.Fatalf("resolveBaseURL = %q", got)
	}
	if got := resolveBaseURL(nil); got != defaultBaseURL {
		t.Fatalf("default base URL = %q", got)
	}
}

func TestRedactURL(t *testing.T) {
	if got := redactURL("https://user:pass@example.test/v1/messages?beta=true"); got != "https://example.test/v1/messages" {
		t.Fatalf("redactURL parsed = %q", got)
	}
	if got := redactURL("http://%zz/path?secret=yes"); got != "http://%zz/path" {
		t.Fatalf("redactURL fallback = %q", got)
	}
}

func TestPreprocessBody(t *testing.T) {
	if got := preprocessBody(nil); got != nil {
		t.Fatalf("nil body = %s", got)
	}

	out := preprocessBody([]byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":[{"type":"text","text":""},{"type":"text","text":"hi"}]}]}`))
	if got := gjson.GetBytes(out, "model").String(); got != "claude-sonnet-4-5-20250929" {
		t.Fatalf("normalized model = %q; body=%s", got, out)
	}
	if got := gjson.GetBytes(out, "max_tokens").Int(); got != 4096 {
		t.Fatalf("max_tokens = %d; body=%s", got, out)
	}
	if strings.Contains(string(out), `"text":""`) {
		t.Fatalf("empty text block survived: %s", out)
	}

	withMax := preprocessBody([]byte(`{"max_tokens":12}`))
	if got := gjson.GetBytes(withMax, "max_tokens").Int(); got != 12 {
		t.Fatalf("existing max_tokens changed to %d", got)
	}
}

func TestPreprocessOAuthBodyRewritesAndAugments(t *testing.T) {
	account := &sdk.Account{
		ID: 7,
		Credentials: map[string]string{
			"account_uuid": "acct-uuid",
		},
	}
	body := []byte(`{"model":"claude-opus-4-8","system":"custom instructions","temperature":0.7,"tool_choice":{"type":"auto"},"messages":[{"role":"user","content":"hi"}]}`)

	out := preprocessOAuthBody(body, account)

	system := gjson.GetBytes(out, "system")
	if !system.IsArray() || !strings.Contains(system.Get("0.text").String(), "Claude Code") {
		t.Fatalf("system prompt was not rewritten: %s", out)
	}
	messages := gjson.GetBytes(out, "messages").Array()
	if len(messages) != 3 {
		t.Fatalf("system instructions were not injected into messages: %s", out)
	}
	userIDRaw := gjson.GetBytes(out, "metadata.user_id").String()
	if userIDRaw == "" {
		t.Fatalf("metadata.user_id missing: %s", out)
	}
	var userID map[string]string
	if err := json.Unmarshal([]byte(userIDRaw), &userID); err != nil {
		t.Fatalf("metadata.user_id is not JSON: %v (%q)", err, userIDRaw)
	}
	if userID["account_uuid"] != "acct-uuid" || userID["device_id"] != newDeviceID(account.ID) || userID["session_id"] == "" {
		t.Fatalf("unexpected metadata.user_id payload: %#v", userID)
	}
	if !gjson.GetBytes(out, "tools").IsArray() {
		t.Fatalf("tools array was not added: %s", out)
	}
	if gjson.GetBytes(out, "temperature").Exists() || gjson.GetBytes(out, "tool_choice").Exists() {
		t.Fatalf("temperature/tool_choice should be removed when tools empty: %s", out)
	}
}

func TestPreprocessOAuthBodyKeepsExistingCCSystemAndTools(t *testing.T) {
	account := &sdk.Account{ID: 8, Credentials: map[string]string{}}
	body := []byte(`{"system":"You are Claude Code, Anthropic's official CLI for Claude.","metadata":{"user_id":"existing"},"tools":[{"name":"a"},{"name":"b","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hi"}]}`)

	out := preprocessOAuthBody(body, account)

	if got := gjson.GetBytes(out, "system").String(); got != claudeCodeSystemPrompt {
		t.Fatalf("existing CC system should stay string, got %q", got)
	}
	if got := gjson.GetBytes(out, "metadata.user_id").String(); got != "existing" {
		t.Fatalf("existing metadata.user_id changed: %q", got)
	}
	if !gjson.GetBytes(out, "tools.1.cache_control").Exists() {
		t.Fatalf("existing cache_control was removed: %s", out)
	}
}

func TestAppendToolsEphemeralCache(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want bool
	}{
		{"missing tools", []byte(`{}`), false},
		{"empty tools", []byte(`{"tools":[]}`), false},
		{"adds to last", []byte(`{"tools":[{"name":"a"},{"name":"b"}]}`), true},
		{"keeps existing", []byte(`{"tools":[{"name":"a","cache_control":{"type":"ephemeral"}}]}`), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := appendToolsEphemeralCache(tc.body)
			got := gjson.GetBytes(out, "tools.0.cache_control").Exists() || gjson.GetBytes(out, "tools.1.cache_control").Exists()
			if got != tc.want {
				t.Fatalf("cache_control exists = %v, want %v; body=%s", got, tc.want, out)
			}
		})
	}
}

func TestHandleModelsRequest(t *testing.T) {
	g := &AnthropicGateway{}
	rec := httptest.NewRecorder()
	outcome := g.handleModelsRequest(&sdk.ForwardRequest{Writer: rec})

	if outcome.Kind != sdk.OutcomeSuccess || outcome.Upstream.StatusCode != http.StatusOK {
		t.Fatalf("outcome = %#v", outcome)
	}
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("recorder code/header = %d/%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), defaultModelList[0].ID) {
		t.Fatalf("models response missing default model: %s", rec.Body.String())
	}
}

func TestHandleErrorResponseAndRejectNonCCRequest(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"5"}},
		Body:       http.NoBody,
	}
	outcome := handleErrorResponse(resp, nil, time.Now().Add(-time.Millisecond))
	if outcome.Kind != sdk.OutcomeAccountRateLimited || outcome.RetryAfter != 5*time.Second {
		t.Fatalf("error outcome = %#v", outcome)
	}

	reject := rejectNonCCRequest(nil, "bad user-agent", time.Now())
	if reject.Kind != sdk.OutcomeClientError || reject.Upstream.StatusCode != http.StatusForbidden {
		t.Fatalf("reject outcome = %#v", reject)
	}
	if !strings.Contains(string(reject.Upstream.Body), "bad user-agent") {
		t.Fatalf("reject body = %s", reject.Upstream.Body)
	}
}

func TestBuildCountTokensHeaders(t *testing.T) {
	t.Run("apikey", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "https://example.test", nil)
		account := &sdk.Account{Type: "apikey", Credentials: map[string]string{"api_key": "sk"}}

		buildCountTokensHeaders(req, account)

		if got := req.Header.Get("Authorization"); got != "Bearer sk" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := req.Header.Get("x-api-key"); got != "sk" {
			t.Fatalf("x-api-key = %q", got)
		}
		if got := req.Header.Get("anthropic-beta"); !strings.Contains(got, BetaTokenCounting) {
			t.Fatalf("beta = %q", got)
		}
	})

	t.Run("oauth", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "https://example.test", nil)
		account := &sdk.Account{Type: "oauth", Credentials: map[string]string{"access_token": "tok"}}

		buildCountTokensHeaders(req, account)

		if got := req.Header["authorization"]; len(got) != 1 || got[0] != "Bearer tok" {
			t.Fatalf("authorization = %#v", got)
		}
		if got := req.Header["anthropic-beta"][0]; got != CountTokensBetaHeader {
			t.Fatalf("beta = %q", got)
		}
		if got := req.Header["Accept"][0]; got != "application/json" {
			t.Fatalf("Accept = %q", got)
		}
	})
}
