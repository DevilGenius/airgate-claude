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

func TestSidecarPollRunCountTokensAndProbe(t *testing.T) {
	usageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Authorization"), "bad") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":1}}`))
	}))
	defer usageServer.Close()
	withUsageEndpoint(t, usageServer.URL)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages/count_tokens" && r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected upstream path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"model":"claude-opus-4-8","usage":{"input_tokens":1}}`))
	}))
	defer upstream.Close()

	g := &AnthropicGateway{
		logger:   testLogger(),
		stdPool:  NewStandardTransportPool(),
		fpPool:   NewFingerprintTransportPool(),
		registry: newAccountRegistry(),
	}
	s := newSidecarRunner(g)

	s.pollOne(context.Background(), &accountSnapshot{id: 1, credentials: map[string]string{"access_token": "good"}}, testLogger())
	s.pollOne(context.Background(), &accountSnapshot{id: 2, credentials: map[string]string{"access_token": "bad"}}, testLogger())

	account := &sdk.Account{ID: 3, Type: "apikey", Credentials: map[string]string{"api_key": "sk", "base_url": upstream.URL}}
	g.registry.register(&sdk.Account{ID: 3, Type: "oauth", Credentials: map[string]string{"access_token": "tok", "base_url": upstream.URL}})
	g.registry.accounts[4] = &accountSnapshot{id: 4, accountType: account.Type, credentials: account.Credentials}
	s.runCountTokens(context.Background(), countTokensJob{accountID: 4, body: []byte(`{"model":"claude-opus-4-8"}`)}, testLogger())

	s.runProbe(context.Background(), &accountSnapshot{
		id:          5,
		accountType: "oauth",
		credentials: map[string]string{"access_token": "tok", "base_url": upstream.URL},
	})
}

func TestSidecarFireRefreshProbeActiveAndLimit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"claude-haiku-4-5-20251001","usage":{"input_tokens":1}}`))
	}))
	defer upstream.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := &AnthropicGateway{
		logger:  testLogger(),
		stdPool: NewStandardTransportPool(),
		fpPool:  NewFingerprintTransportPool(),
	}
	s := newSidecarRunner(g)
	s.ctx = ctx
	s.cancel = cancel
	s.stopped = false
	s.probeCh = make(chan struct{}, 1)

	s.fireRefreshProbe(&sdk.Account{ID: 6, Type: "oauth", Credentials: map[string]string{"access_token": "tok", "base_url": upstream.URL}})
	waitDone := make(chan struct{})
	go func() {
		s.probes.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("probe did not finish")
	}

	s.probeCh <- struct{}{}
	s.fireRefreshProbe(&sdk.Account{ID: 7, Type: "oauth", Credentials: map[string]string{"access_token": "tok", "base_url": upstream.URL}})
	<-s.probeCh
}
