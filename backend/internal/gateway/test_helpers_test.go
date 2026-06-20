package gateway

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestGateway(t *testing.T) *AnthropicGateway {
	t.Helper()
	g := &AnthropicGateway{logger: testLogger()}
	if err := g.Init(nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := g.Stop(t.Context()); err != nil {
			t.Fatalf("Stop returned error: %v", err)
		}
	})
	return g
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}
