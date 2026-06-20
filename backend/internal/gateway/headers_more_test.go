package gateway

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestSetRawHeaderPreservesCaseAndRejectsControls(t *testing.T) {
	h := http.Header{"Authorization": []string{"old"}}
	setRawHeader(h, "authorization", "Bearer token")

	if _, ok := h["Authorization"]; ok {
		t.Fatalf("canonical Authorization key was not removed: %#v", h)
	}
	if got := h["authorization"]; len(got) != 1 || got[0] != "Bearer token" {
		t.Fatalf("raw authorization header = %#v", got)
	}

	setRawHeader(h, "x-bad\nkey", "value")
	if _, ok := h["x-bad\nkey"]; ok {
		t.Fatalf("header with newline key should be rejected")
	}
	setRawHeader(h, "x-bad-value", "bad\r\nvalue")
	if _, ok := h["x-bad-value"]; ok {
		t.Fatalf("header with newline value should be rejected")
	}
}

func TestBetaFilteringAndMerging(t *testing.T) {
	input := strings.Join([]string{
		BetaOAuth,
		" ",
		BetaFastMode,
		BetaClaudeCode,
		BetaSkills,
		"custom-beta",
	}, ",")
	filtered := filterDroppedBetas(input)
	if strings.Contains(filtered, BetaFastMode) || strings.Contains(filtered, BetaSkills) {
		t.Fatalf("filtered beta still contains dropped token: %q", filtered)
	}
	if filtered != strings.Join([]string{BetaOAuth, BetaClaudeCode, "custom-beta"}, ",") {
		t.Fatalf("filtered beta = %q", filtered)
	}

	merged := mergeBetas("a,b,a", " b, c,,a ")
	if merged != "a,b,c" {
		t.Fatalf("mergeBetas = %q, want a,b,c", merged)
	}
}

func TestIsRawCaseHeaderAndHaikuModel(t *testing.T) {
	if !isRawCaseHeader("Authorization") || !isRawCaseHeader("x-app") || !isRawCaseHeader("Content-Type") {
		t.Fatalf("expected known raw-case headers to be true")
	}
	if isRawCaseHeader("X-Stainless-Retry-Count") {
		t.Fatalf("retry count should use canonical Header.Set casing")
	}
	if !isHaikuModel("CLAUDE-HAIKU-4-5") || isHaikuModel("claude-opus-4-8") {
		t.Fatalf("isHaikuModel returned unexpected result")
	}
}

func TestSetAnthropicAuthHeadersAPIKey(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.test/v1/messages", nil)
	if err != nil {
		t.Fatal(err)
	}
	account := &sdk.Account{Type: "apikey", Credentials: map[string]string{"api_key": "sk-ant-test"}}
	clientHeaders := http.Header{
		"Anthropic-Beta":    []string{BetaOAuth + "," + BetaFastMode + ",custom-beta"},
		"Anthropic-Version": []string{"2024-01-01"},
	}

	setAnthropicAuthHeaders(req, account, clientHeaders, "claude-opus-4-8")

	if got := req.Header["x-api-key"]; len(got) != 1 || got[0] != "sk-ant-test" {
		t.Fatalf("x-api-key = %#v", got)
	}
	beta := req.Header["anthropic-beta"][0]
	if strings.Contains(beta, BetaFastMode) || beta != BetaOAuth+",custom-beta" {
		t.Fatalf("anthropic-beta = %q", beta)
	}
	if got := req.Header["anthropic-version"][0]; got != "2024-01-01" {
		t.Fatalf("anthropic-version = %q", got)
	}
	if got := req.Header["content-type"][0]; got != "application/json" {
		t.Fatalf("content-type = %q", got)
	}
	if retry := req.Header.Get("X-Stainless-Retry-Count"); !slices.Contains([]string{"0", "1", "2"}, retry) {
		t.Fatalf("retry count = %q", retry)
	}
	if got := req.Header.Get("User-Agent"); !strings.Contains(got, "claude-cli/") {
		t.Fatalf("User-Agent = %q", got)
	}
}

func TestSetAnthropicAuthHeadersDefaultsAndOAuth(t *testing.T) {
	t.Run("apikey haiku default beta", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "https://example.test", nil)
		account := &sdk.Account{Type: "apikey", Credentials: map[string]string{"api_key": "sk"}}

		setAnthropicAuthHeaders(req, account, http.Header{}, "claude-haiku-4-5")

		if got := req.Header["anthropic-beta"][0]; got != APIKeyHaikuBetaHeader {
			t.Fatalf("haiku api-key beta = %q", got)
		}
	})

	t.Run("oauth merges safe client beta", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "https://example.test", nil)
		account := &sdk.Account{Type: "oauth", Credentials: map[string]string{"access_token": "tok"}}
		clientHeaders := http.Header{"Anthropic-Beta": []string{BetaFastMode + ",custom-beta"}}

		setAnthropicAuthHeaders(req, account, clientHeaders, "claude-opus-4-8")

		if got := req.Header["authorization"]; len(got) != 1 || got[0] != "Bearer tok" {
			t.Fatalf("authorization = %#v", got)
		}
		beta := req.Header["anthropic-beta"][0]
		if !strings.Contains(beta, OAuthBetaHeader) || !strings.Contains(beta, "custom-beta") || strings.Contains(beta, BetaFastMode) {
			t.Fatalf("oauth beta = %q", beta)
		}
	})

	t.Run("oauth haiku uses haiku beta", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "https://example.test", nil)
		account := &sdk.Account{Type: "session_key", Credentials: map[string]string{"access_token": "tok"}}

		setAnthropicAuthHeaders(req, account, http.Header{}, "claude-haiku-4-5")

		if got := req.Header["anthropic-beta"][0]; got != HaikuBetaHeader {
			t.Fatalf("haiku oauth beta = %q", got)
		}
	})
}

func TestPickRetryCountReturnsAllowedValues(t *testing.T) {
	for i := 0; i < 100; i++ {
		if got := pickRetryCount(); !slices.Contains([]string{"0", "1", "2"}, got) {
			t.Fatalf("pickRetryCount = %q", got)
		}
	}
}
