package gateway

import (
	"context"
	"log/slog"
	"testing"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestAccountRegistryRegisterSnapshotAndCleanup(t *testing.T) {
	registry := newAccountRegistry()
	registry.register(nil)
	registry.register(&sdk.Account{ID: 1, Type: "apikey", Credentials: map[string]string{"api_key": "sk"}})
	if len(registry.accounts) != 0 {
		t.Fatalf("nil/apikey accounts should not be registered")
	}

	creds := map[string]string{"access_token": "tok"}
	account := &sdk.Account{ID: 2, Type: "oauth", ProxyURL: "http://proxy", Credentials: creds}
	registry.register(account)
	creds["access_token"] = "mutated"

	snaps := registry.snapshot()
	if len(snaps) != 1 {
		t.Fatalf("snapshot count = %d", len(snaps))
	}
	if snaps[0].credentials["access_token"] != "tok" || snaps[0].proxyURL != "http://proxy" {
		t.Fatalf("snapshot did not copy account: %#v", snaps[0])
	}

	registry.accounts[3] = &accountSnapshot{id: 3, lastSeenAt: time.Now().Add(-accountRegistryIdleTTL - time.Hour)}
	registry.lastCleanupTime = time.Now().Add(-accountRegistryCleanupInterval - time.Second)
	registry.cleanupLocked(time.Now())
	if _, ok := registry.accounts[3]; ok {
		t.Fatalf("cleanupLocked left idle account")
	}

	registry.accounts = map[int64]*accountSnapshot{
		1: {id: 1, lastSeenAt: time.Now().Add(-time.Hour)},
		2: {id: 2, lastSeenAt: time.Now()},
	}
	registry.deleteOldestLocked()
	if _, ok := registry.accounts[1]; ok {
		t.Fatalf("deleteOldestLocked did not remove oldest")
	}
}

func TestSidecarScheduleCountTokens(t *testing.T) {
	s := newSidecarRunner(&AnthropicGateway{logger: testLogger()})
	s.scheduleCountTokens(1, []byte(`{"x":1}`))
	if len(s.jobs) != 0 {
		t.Fatalf("stopped sidecar should not enqueue jobs")
	}

	s.stopped = false
	s.jobs = make(chan countTokensJob, 1)
	body := []byte(`{"x":1}`)
	s.scheduleCountTokens(7, body)
	body[0] = '['

	select {
	case job := <-s.jobs:
		if job.accountID != 7 || string(job.body) != `{"x":1}` {
			t.Fatalf("job = %#v body=%s", job, job.body)
		}
	default:
		t.Fatalf("expected queued job")
	}

	s.jobs = make(chan countTokensJob)
	s.scheduleCountTokens(7, []byte(`{"x":1}`))
	if len(s.jobs) != 0 {
		t.Fatalf("full unbuffered channel should drop job")
	}

	s.scheduleCountTokens(7, nil)
}

func TestSidecarStartStopAndNoopWorkers(t *testing.T) {
	g := &AnthropicGateway{logger: testLogger(), registry: newAccountRegistry()}
	s := newSidecarRunner(g)
	s.start()
	s.start()
	s.stop()
	s.stop()
	if !s.stopped {
		t.Fatalf("sidecar should be stopped")
	}

	s.pollOne(context.Background(), &accountSnapshot{id: 1, credentials: map[string]string{}}, slog.Default())
	s.runCountTokens(context.Background(), countTokensJob{accountID: 99, body: []byte(`{}`)}, slog.Default())
	s.fireRefreshProbe(nil)
	s.fireRefreshProbe(&sdk.Account{ID: 1, Type: "oauth", Credentials: map[string]string{"access_token": "tok"}})
}
