package gateway

import (
	"net/http"
	"testing"
	"time"
)

func TestStandardTransportPoolLifecycle(t *testing.T) {
	pool := NewStandardTransportPool()
	t1 := pool.Get("")
	t2 := pool.Get("")
	if t1 != t2 {
		t.Fatalf("same proxy should reuse transport")
	}

	proxyTransport := pool.Get("http://127.0.0.1:8080")
	if proxyTransport.Proxy == nil {
		t.Fatalf("proxy transport should set Proxy")
	}

	pool.pool["old"] = &standardTransportEntry{transport: &http.Transport{}, lastUsedAt: time.Now().Add(-standardTransportIdleTTL - time.Hour)}
	pool.lastCleanupTime = time.Now().Add(-standardTransportCleanupInterval - time.Second)
	pool.cleanupIdleLocked(time.Now())
	if _, ok := pool.pool["old"]; ok {
		t.Fatalf("cleanupIdleLocked left idle transport")
	}

	pool.pool = map[string]*standardTransportEntry{
		"old": {transport: &http.Transport{}, lastUsedAt: time.Now().Add(-time.Hour)},
		"new": {transport: &http.Transport{}, lastUsedAt: time.Now()},
	}
	pool.deleteOldestLocked()
	if _, ok := pool.pool["old"]; ok {
		t.Fatalf("deleteOldestLocked did not remove oldest")
	}

	pool.Close()
	if len(pool.pool) != 0 {
		t.Fatalf("Close left pool entries")
	}
}

func TestFingerprintTransportPoolLifecycle(t *testing.T) {
	if got := fpKey(12, "proxy", "profile"); got != "12|proxy|profile" {
		t.Fatalf("fpKey = %q", got)
	}

	pool := NewFingerprintTransportPool()
	t1 := pool.Get(1, "", "")
	t2 := pool.Get(1, "", "")
	if t1 != t2 {
		t.Fatalf("same fingerprint key should reuse transport")
	}
	t3 := pool.Get(2, "", "")
	if t3 == t1 {
		t.Fatalf("different account should get different transport")
	}

	pool.RemoveAccount(1)
	for key := range pool.pool {
		if key == fpKey(1, "", "") {
			t.Fatalf("RemoveAccount left account 1 entry")
		}
	}
	pool.RemoveAccount(999)
	(*FingerprintTransportPool)(nil).RemoveAccount(1)

	pool.pool["old"] = &fingerprintTransportEntry{transport: &http.Transport{}, lastUsedAt: time.Now().Add(-fingerprintTransportIdleTTL - time.Hour)}
	pool.lastCleanupTime = time.Now().Add(-fingerprintTransportCleanupInterval - time.Second)
	pool.cleanupIdleLocked(time.Now())
	if _, ok := pool.pool["old"]; ok {
		t.Fatalf("cleanupIdleLocked left idle fingerprint transport")
	}

	pool.pool = map[string]*fingerprintTransportEntry{
		"old": {transport: &http.Transport{}, lastUsedAt: time.Now().Add(-time.Hour)},
		"new": {transport: &http.Transport{}, lastUsedAt: time.Now()},
	}
	pool.deleteOldestLocked()
	if _, ok := pool.pool["old"]; ok {
		t.Fatalf("deleteOldestLocked did not remove oldest fingerprint transport")
	}

	pool.Close()
	if len(pool.pool) != 0 {
		t.Fatalf("Close left fingerprint pool entries")
	}
}

func TestPoolStatsAndHTTPClientWithPools(t *testing.T) {
	stdPool := NewStandardTransportPool()
	fpPool := NewFingerprintTransportPool()
	stdPool.Get("")
	fpPool.Get(1, "", "")

	if got := poolStats(stdPool, fpPool); got != "standard=1, fingerprint=1" {
		t.Fatalf("poolStats = %q", got)
	}
	if got := poolStats(nil, nil); got != "standard=0, fingerprint=0" {
		t.Fatalf("poolStats nil = %q", got)
	}

	client := getHTTPClient(stdPool, fpPool, 1, "oauth", "", "", false)
	if client.Transport == nil || client.Timeout != httpTimeout {
		t.Fatalf("oauth client = %#v", client)
	}
	streamClient := getHTTPClient(stdPool, fpPool, 1, "apikey", "", "", true)
	if streamClient.Timeout != 0 {
		t.Fatalf("stream client timeout = %s", streamClient.Timeout)
	}
}
