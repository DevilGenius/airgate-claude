package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestClassifyHTTPFailure(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
		want    sdk.OutcomeKind
	}{
		{"success", 200, "", sdk.OutcomeSuccess},
		{"rate limited", 429, "slow down", sdk.OutcomeAccountRateLimited},
		{"unauthorized", 401, "bad key", sdk.OutcomeAccountDead},
		{"forbidden", 403, "denied", sdk.OutcomeAccountDead},
		{"disabled account text", 400, "account disabled", sdk.OutcomeAccountDead},
		{"deactivated account text", 400, "user was deactivated", sdk.OutcomeAccountDead},
		{"suspended account text", 400, "workspace suspended", sdk.OutcomeAccountDead},
		{"ordinary client error", 400, "schema error", sdk.OutcomeClientError},
		{"server error", 503, "unavailable", sdk.OutcomeUpstreamTransient},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyHTTPFailure(tc.status, tc.message); got != tc.want {
				t.Fatalf("classifyHTTPFailure() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestExtractErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{"empty", nil, ""},
		{"type and message", []byte(`{"error":{"type":"invalid_request_error","message":"bad body"}}`), "invalid_request_error: bad body"},
		{"message only", []byte(`{"error":{"message":"bad body"}}`), "bad body"},
		{"type only", []byte(`{"error":{"type":"rate_limit_error"}}`), "rate_limit_error"},
		{"not anthropic error", []byte(`{"message":"ignored"}`), ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractErrorMessage(tc.body); got != tc.want {
				t.Fatalf("extractErrorMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRetryAfterParsing(t *testing.T) {
	headers := http.Header{}
	if got := extractRetryAfterHeader(headers); got != 0 {
		t.Fatalf("empty Retry-After = %s, want 0", got)
	}

	headers.Set("Retry-After", "17")
	if got := extractRetryAfterHeader(headers); got != 17*time.Second {
		t.Fatalf("numeric Retry-After = %s, want 17s", got)
	}

	headers.Set("Retry-After", "retry after 23s")
	if got := extractRetryAfterHeader(headers); got != 23*time.Second {
		t.Fatalf("text Retry-After = %s, want 23s", got)
	}

	if got := parseRetryDelay("no delay"); got != 0 {
		t.Fatalf("parseRetryDelay(no delay) = %s, want 0", got)
	}
}

func TestTruncateAndReadLimitedErrorBody(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("truncate short = %q", got)
	}
	if got := truncate("0123456789", 4); got != "0123..." {
		t.Fatalf("truncate long = %q", got)
	}

	body, err := readLimitedErrorBody(strings.NewReader(strings.Repeat("a", maxErrorResponseBodyBytes+10)))
	if err != nil {
		t.Fatalf("readLimitedErrorBody returned error: %v", err)
	}
	if len(body) != maxErrorResponseBodyBytes+1 {
		t.Fatalf("limited body length = %d, want %d", len(body), maxErrorResponseBodyBytes+1)
	}
}

func TestJSONHelpers(t *testing.T) {
	var errPayload map[string]string
	if err := json.Unmarshal(jsonError("boom"), &errPayload); err != nil {
		t.Fatalf("jsonError produced invalid JSON: %v", err)
	}
	if errPayload["error"] != "boom" {
		t.Fatalf("jsonError payload = %#v", errPayload)
	}

	if got := string(jsonMarshal(map[string]string{"ok": "yes"})); got != `{"ok":"yes"}` {
		t.Fatalf("jsonMarshal = %s", got)
	}
}

func TestOutcomeHelpers(t *testing.T) {
	headers := http.Header{"X-Test": []string{"1"}}
	usage := &sdk.Usage{Model: "claude-opus-4-8"}

	success := successOutcome(201, []byte("ok"), headers, usage)
	if success.Kind != sdk.OutcomeSuccess || success.Upstream.StatusCode != 201 || success.Usage != usage {
		t.Fatalf("unexpected success outcome: %#v", success)
	}

	failure := failureOutcome(429, []byte("limited"), headers, "rate_limit_error", 2*time.Second)
	if failure.Kind != sdk.OutcomeAccountRateLimited || failure.RetryAfter != 2*time.Second {
		t.Fatalf("unexpected failure outcome: %#v", failure)
	}
	if failure.Reason != "HTTP 429: rate_limit_error" {
		t.Fatalf("failure reason = %q", failure.Reason)
	}

	noMessage := failureOutcome(400, nil, nil, "", 0)
	if noMessage.Reason != "" {
		t.Fatalf("empty-message failure reason = %q, want empty", noMessage.Reason)
	}

	transient := transientOutcome("dial failed")
	if transient.Kind != sdk.OutcomeUpstreamTransient || transient.Upstream.StatusCode != http.StatusBadGateway {
		t.Fatalf("unexpected transient outcome: %#v", transient)
	}

	dead := accountDeadOutcome("missing token")
	if dead.Kind != sdk.OutcomeAccountDead || dead.Upstream.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected account-dead outcome: %#v", dead)
	}

	aborted := streamAbortedOutcome(200, "broken pipe", usage)
	if aborted.Kind != sdk.OutcomeStreamAborted || aborted.Usage != usage || aborted.Reason != "broken pipe" {
		t.Fatalf("unexpected stream-aborted outcome: %#v", aborted)
	}
}
