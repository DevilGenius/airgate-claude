package gateway

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
)

func TestBunClientHelloSpec(t *testing.T) {
	spec := buildBunClientHelloSpec()
	if spec.TLSVersMin != utls.VersionTLS12 || spec.TLSVersMax != utls.VersionTLS13 {
		t.Fatalf("TLS versions = %x/%x", spec.TLSVersMin, spec.TLSVersMax)
	}
	if len(spec.CipherSuites) != len(defaultCipherSuites) || len(spec.Extensions) == 0 {
		t.Fatalf("unexpected spec sizes: ciphers=%d extensions=%d", len(spec.CipherSuites), len(spec.Extensions))
	}
	if _, ok := spec.Extensions[0].(*utls.SNIExtension); !ok {
		t.Fatalf("first extension = %T, want SNI", spec.Extensions[0])
	}
	if exported := ExportBunClientHelloSpec(); exported.TLSVersMax != spec.TLSVersMax {
		t.Fatalf("ExportBunClientHelloSpec mismatch")
	}
	if selected := selectClientHelloSpec("unknown"); selected.TLSVersMax != spec.TLSVersMax {
		t.Fatalf("select unknown profile mismatch")
	}
	if selected := selectClientHelloSpec("bun-2.1.112"); selected.TLSVersMax != spec.TLSVersMax {
		t.Fatalf("select fixed profile mismatch")
	}
}

func TestFingerprintTransportAndProxyHelpers(t *testing.T) {
	transport := buildFingerprintTransportWithProfile("http://127.0.0.1:8080", "bun-2.1.112")
	if transport.Proxy == nil || transport.DialTLSContext == nil || transport.ForceAttemptHTTP2 {
		t.Fatalf("fingerprint transport not configured as expected: %#v", transport)
	}
	if direct := buildFingerprintTransportWithProfile("", ""); direct.Proxy != nil || direct.DialTLSContext == nil {
		t.Fatalf("direct fingerprint transport not configured as expected: %#v", direct)
	}

	if !hasPort("example.com:443") || hasPort("example.com") {
		t.Fatalf("hasPort returned unexpected result")
	}
	u, _ := url.Parse("http://user:pass@example.test")
	if got := getPassword(u.User); got != "pass" {
		t.Fatalf("password = %q", got)
	}
	if got := basicAuth("user", "pass"); got != base64.StdEncoding.EncodeToString([]byte("user:pass")) {
		t.Fatalf("basicAuth = %q", got)
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
	defer cancel()
	if deadline := proxyHandshakeDeadline(ctx); time.Until(deadline) > time.Second {
		t.Fatalf("deadline did not honor context: %s", deadline)
	}

	if _, err := dialThroughProxy(context.Background(), "://bad", "example.com:443", nil); err == nil {
		t.Fatalf("invalid proxy URL should fail")
	}
}

func TestDialThroughHTTPProxy(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "example.com:443" {
			t.Fatalf("CONNECT request = %s %s host=%s", r.Method, r.URL.String(), r.Host)
		}
		if got := r.Header.Get("Proxy-Authorization"); got != "Basic "+basicAuth("user", "pass") {
			t.Fatalf("Proxy-Authorization = %q", got)
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatalf("response writer does not support hijack")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack failed: %v", err)
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\n\r\nprefetched"))
	}))
	defer proxyServer.Close()

	proxyURL := strings.Replace(proxyServer.URL, "http://", "http://user:pass@", 1)
	conn, err := dialThroughProxy(context.Background(), proxyURL, "example.com:443", &net.Dialer{Timeout: time.Second})
	if err != nil {
		t.Fatalf("dialThroughProxy returned error: %v", err)
	}
	defer func() { _ = conn.Close() }()

	buf := make([]byte, len("prefetched"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read buffered bytes failed: %v", err)
	}
	if string(buf) != "prefetched" {
		t.Fatalf("buffered bytes = %q", buf)
	}
}

func TestDialThroughHTTPProxyNonOKAndDeadlineDialer(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "proxy auth required", http.StatusProxyAuthRequired)
	}))
	defer proxyServer.Close()

	if _, err := dialThroughProxy(context.Background(), proxyServer.URL, "example.com:443", &net.Dialer{Timeout: time.Second}); err == nil || !strings.Contains(err.Error(), "proxy CONNECT failed") {
		t.Fatalf("non-OK proxy error = %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	d := proxyDeadlineDialer{ctx: context.Background(), dialer: &net.Dialer{Timeout: time.Second}}
	conn, err := d.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("proxyDeadlineDialer.Dial returned error: %v", err)
	}
	_ = conn.Close()
	<-done
}
