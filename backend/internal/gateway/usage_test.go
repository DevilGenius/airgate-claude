package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func withUsageEndpoint(t *testing.T, endpoint string) {
	t.Helper()
	old := usageAPIEndpoint
	usageAPIEndpoint = endpoint
	t.Cleanup(func() { usageAPIEndpoint = old })
}

func TestFetchUsageAndQueryQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage" {
			t.Fatalf("usage path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer access" || r.Header.Get("anthropic-beta") != BetaOAuth {
			t.Fatalf("usage headers = %#v", r.Header)
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":12.34,"resets_at":"2026-06-20T01:00:00Z"},"seven_day":{"utilization":56.78,"resets_at":"2026-06-21T01:00:00Z"},"seven_day_sonnet":{"utilization":9.87,"resets_at":"2026-06-22T01:00:00Z"}}`))
	}))
	defer server.Close()
	withUsageEndpoint(t, server.URL+"/usage")

	g := &AnthropicGateway{}
	usage, err := g.fetchUsage(context.Background(), 1, "access", "", "")
	if err != nil {
		t.Fatalf("fetchUsage returned error: %v", err)
	}
	if usage.FiveHour.Utilization != 12.34 || usage.SevenDaySonnet.ResetsAt == "" {
		t.Fatalf("usage response = %#v", usage)
	}

	quota, err := g.QueryQuota(context.Background(), map[string]string{"access_token": "access"})
	if err != nil {
		t.Fatalf("QueryQuota returned error: %v", err)
	}
	if quota.Extra["five_hour_utilization"] != "12.34" || quota.Extra["seven_day_sonnet_utilization"] != "9.87" {
		t.Fatalf("quota extra = %#v", quota.Extra)
	}
}

func TestQueryQuotaWithoutAccessToken(t *testing.T) {
	g := &AnthropicGateway{}
	if _, err := g.QueryQuota(context.Background(), map[string]string{}); err != sdk.ErrNotSupported {
		t.Fatalf("QueryQuota without token error = %v", err)
	}
}

func TestFetchUsageErrors(t *testing.T) {
	t.Run("non ok", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(strings.Repeat("x", 300)))
		}))
		defer server.Close()
		withUsageEndpoint(t, server.URL)

		g := &AnthropicGateway{}
		if _, err := g.fetchUsage(context.Background(), 1, "access", "", ""); err == nil || !strings.Contains(err.Error(), "HTTP 503") {
			t.Fatalf("non-OK fetchUsage error = %v", err)
		}
	})

	t.Run("decode", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{`))
		}))
		defer server.Close()
		withUsageEndpoint(t, server.URL)

		g := &AnthropicGateway{}
		if _, err := g.fetchUsage(context.Background(), 1, "access", "", ""); err == nil || !strings.Contains(err.Error(), "解析 usage 响应失败") {
			t.Fatalf("decode fetchUsage error = %v", err)
		}
	})

	t.Run("bad endpoint", func(t *testing.T) {
		withUsageEndpoint(t, "http://%zz")
		g := &AnthropicGateway{}
		if _, err := g.fetchUsage(context.Background(), 1, "access", "", ""); err == nil {
			t.Fatalf("bad endpoint should fail")
		}
	})
}

func TestUsageTransport(t *testing.T) {
	g := &AnthropicGateway{fpPool: NewFingerprintTransportPool()}
	if rt := g.usageTransport(1, "", ""); rt == nil {
		t.Fatalf("usageTransport with pool returned nil")
	}
	if rt := (*AnthropicGateway)(nil).usageTransport(1, "", ""); rt == nil {
		t.Fatalf("usageTransport nil gateway returned nil")
	}
}

func TestHandleRequestUsageAccountsSuccessAndError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "bad" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"bad token"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":1,"resets_at":"2026-06-20T01:00:00Z"},"seven_day":{"utilization":2},"seven_day_sonnet":{"utilization":3,"resets_at":"2026-06-21T01:00:00Z"}}`))
	}))
	defer server.Close()
	withUsageEndpoint(t, server.URL)

	g := &AnthropicGateway{logger: testLogger()}
	body := []byte(`[{"id":1,"credentials":{"access_token":"good"}},{"id":2,"credentials":{"access_token":"bad"}}]`)
	status, _, respBody, err := g.HandleRequest(context.Background(), "", "usage/accounts", "", nil, body)
	if err != nil || status != http.StatusOK {
		t.Fatalf("usage/accounts status/error = %d/%v", status, err)
	}
	var resp accountUsageAccountsResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("response JSON error: %v", err)
	}
	if len(resp.Accounts["1"].Windows) != 3 {
		t.Fatalf("account 1 windows = %#v", resp.Accounts["1"].Windows)
	}
	if len(resp.Errors) != 1 || resp.Errors[0].ID != 2 {
		t.Fatalf("errors = %#v", resp.Errors)
	}
}
