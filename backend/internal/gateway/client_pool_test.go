package gateway

import (
	"testing"
	"time"
)

func TestClientPoolLifecycle(t *testing.T) {
	if client := (*clientPool)(nil).get(""); client == nil {
		t.Fatalf("nil pool get returned nil")
	} else {
		closeReqClient(client)
	}
	closeReqClient(nil)

	pool := &clientPool{clients: map[string]*clientEntry{}, lastCleanupTime: time.Now()}
	c1 := pool.get("")
	c2 := pool.get("")
	if c1 == nil || c1 != c2 {
		t.Fatalf("client pool did not reuse client")
	}

	proxied := pool.get("http://127.0.0.1:8080")
	if proxied == nil {
		t.Fatalf("proxied client is nil")
	}

	pool.clients["old"] = &clientEntry{client: newReqClient(""), lastUsedAt: time.Now().Add(-clientPoolIdleTTL - time.Hour)}
	pool.lastCleanupTime = time.Now().Add(-clientPoolCleanupInterval - time.Second)
	pool.cleanupIdleLocked(time.Now())
	if _, ok := pool.clients["old"]; ok {
		t.Fatalf("cleanupIdleLocked left idle client")
	}

	pool.clients = map[string]*clientEntry{
		"old": {client: newReqClient(""), lastUsedAt: time.Now().Add(-time.Hour)},
		"new": {client: newReqClient(""), lastUsedAt: time.Now()},
	}
	pool.deleteOldestLocked()
	if _, ok := pool.clients["old"]; ok {
		t.Fatalf("deleteOldestLocked did not remove oldest")
	}

	pool.close()
	if len(pool.clients) != 0 {
		t.Fatalf("close left clients: %#v", pool.clients)
	}
	(*clientPool)(nil).close()
}
