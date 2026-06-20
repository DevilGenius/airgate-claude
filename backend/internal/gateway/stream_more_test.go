package gateway

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestSSEHelpers(t *testing.T) {
	if data, ok := extractSSEData(" data: {\"x\":1} "); !ok || data != `{"x":1}` {
		t.Fatalf("extractSSEData = %q/%v", data, ok)
	}
	if data, ok := extractSSEData("event: ping"); ok || data != "" {
		t.Fatalf("non-data extract = %q/%v", data, ok)
	}

	rec := httptest.NewRecorder()
	if err := writeSSEEvent(rec, []string{"event: message", "data: ok", ""}); err != nil {
		t.Fatalf("writeSSEEvent returned error: %v", err)
	}
	if rec.Body.String() != "event: message\ndata: ok\n\n" {
		t.Fatalf("SSE body = %q", rec.Body.String())
	}

	usage := &sdk.Usage{Currency: usageCurrencyUSD}
	var tokens tokenUsage
	var once sync.Once
	observeSSEEvent([]string{`data: {"type":"content_block_delta"}`, `data: {"type":"message_delta","usage":{"output_tokens":3}}`}, time.Now().Add(-10*time.Millisecond), usage, &tokens, &once)
	if usage.FirstTokenMs <= 0 || usage.OutputTokens != 3 {
		t.Fatalf("observe usage = %#v", usage)
	}
}

func TestHandleStreamResponseSuccess(t *testing.T) {
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"model":"claude-opus-4-8","usage":{"input_tokens":10}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"text":"hi"}}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","usage":{"output_tokens":5}}`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    httptest.NewRequest(http.MethodPost, "/v1/messages", nil),
	}
	rec := httptest.NewRecorder()

	outcome, err := handleStreamResponse(resp, rec, time.Now().Add(-20*time.Millisecond))
	if err != nil {
		t.Fatalf("handleStreamResponse returned error: %v", err)
	}
	if outcome.Kind != sdk.OutcomeSuccess || outcome.Usage == nil || outcome.Usage.InputTokens != 10 || outcome.Usage.OutputTokens != 5 {
		t.Fatalf("stream outcome = %#v", outcome)
	}
	if !strings.Contains(rec.Body.String(), "message_start") || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream recorder body/header = %q/%q", rec.Body.String(), rec.Header().Get("Content-Type"))
	}
}

func TestHandleStreamResponseWriteError(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":1}}\n\n")),
		Request:    httptest.NewRequest(http.MethodPost, "/v1/messages", nil),
	}
	outcome, err := handleStreamResponse(resp, failingResponseWriter{}, time.Now())
	if err == nil || outcome.Kind != sdk.OutcomeStreamAborted {
		t.Fatalf("write error outcome/error = %#v/%v", outcome, err)
	}
}

func TestHandleNonStreamResponseErrors(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(errReader{})}
	outcome, err := handleNonStreamResponse(resp, nil, time.Now())
	if err == nil || outcome.Kind != sdk.OutcomeUpstreamTransient {
		t.Fatalf("read error outcome/error = %#v/%v", outcome, err)
	}
}

type failingResponseWriter struct{}

func (failingResponseWriter) Header() http.Header { return http.Header{} }
func (failingResponseWriter) WriteHeader(int)     {}
func (failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
