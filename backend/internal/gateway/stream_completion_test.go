package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type completionClaudeBody struct {
	*strings.Reader
	closed bool
}

func (b *completionClaudeBody) Read(p []byte) (int, error) {
	if b.Len() == 0 {
		return 0, context.Canceled
	}
	return b.Reader.Read(p)
}
func (b *completionClaudeBody) Close() error { b.closed = true; return nil }

type completionClaudeWriter struct {
	*httptest.ResponseRecorder
	marked           bool
	markedBeforeStop bool
}

func (w *completionClaudeWriter) BeginStreamCompletion() { w.marked = true }
func (w *completionClaudeWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "message_stop") {
		w.markedBeforeStop = w.marked
	}
	return w.ResponseRecorder.Write(p)
}

func TestClaudeStopsAtMessageStopAndPreservesUsage(t *testing.T) {
	body := &completionClaudeBody{Reader: strings.NewReader(`data: {"type":"message_start","message":{"model":"claude-opus-4-8","usage":{"input_tokens":10}}}` + "\n\n" + `data: {"type":"message_delta","usage":{"output_tokens":7}}` + "\n\n" + `data: {"type":"message_stop"}` + "\n\n")}
	w := &completionClaudeWriter{ResponseRecorder: httptest.NewRecorder()}
	out, err := handleStreamResponse(&http.Response{StatusCode: 200, Body: body}, w, time.Now())
	if err != nil || out.Kind != sdk.OutcomeSuccess || out.Usage == nil || out.Usage.OutputTokens != 7 || !body.closed || !w.markedBeforeStop {
		t.Fatalf("terminal result lost: %+v %v", out, err)
	}
}

func TestClaudeEOFBeforeStopIsAborted(t *testing.T) {
	body := io.NopCloser(strings.NewReader(`data: {"type":"message_delta","usage":{"output_tokens":7}}` + "\n\n"))
	out, err := handleStreamResponse(&http.Response{StatusCode: 200, Body: body}, httptest.NewRecorder(), time.Now())
	if err == nil || out.Kind != sdk.OutcomeStreamAborted || out.Usage.OutputTokens != 7 {
		t.Fatalf("early EOF became success: %+v %v", out, err)
	}
}
