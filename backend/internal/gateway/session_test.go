package gateway

import (
	"regexp"
	"testing"
	"time"
)

func TestDeviceIDAndUUIDFormat(t *testing.T) {
	id1 := newDeviceID(42)
	id2 := newDeviceID(42)
	if id1 != id2 {
		t.Fatalf("newDeviceID should be deterministic per account: %q != %q", id1, id2)
	}
	if matched := regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(id1); !matched {
		t.Fatalf("device id has unexpected format: %q", id1)
	}

	uuid := newUUIDv4()
	if matched := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(uuid); !matched {
		t.Fatalf("uuid v4 has unexpected format: %q", uuid)
	}
}

func TestConversationFingerprint(t *testing.T) {
	if got := conversationFingerprint(nil); got != "" {
		t.Fatalf("nil fingerprint = %q", got)
	}
	if got := conversationFingerprint([]byte(`{"messages":[]}`)); got != "" {
		t.Fatalf("empty messages fingerprint = %q", got)
	}

	bodyA := []byte(`{"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"stable"},{"role":"user","content":"question A"}]}`)
	bodyB := []byte(`{"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"stable"},{"role":"user","content":"question B"}]}`)
	if gotA, gotB := conversationFingerprint(bodyA), conversationFingerprint(bodyB); gotA == "" || gotA != gotB {
		t.Fatalf("fingerprint should ignore final user turn: %q vs %q", gotA, gotB)
	}

	bodyC := []byte(`{"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"changed"},{"role":"user","content":"question B"}]}`)
	if gotA, gotC := conversationFingerprint(bodyA), conversationFingerprint(bodyC); gotA == gotC {
		t.Fatalf("fingerprint should include second-to-last turn: %q", gotA)
	}
}

func TestSessionCacheStickyUserID(t *testing.T) {
	cache := &sessionCache{entries: map[string]sessionEntry{}, lastCleanupTime: time.Now()}
	id1 := cache.stickyUserID(1, "fp")
	id2 := cache.stickyUserID(1, "fp")
	if id1 == "" || id1 != id2 {
		t.Fatalf("sticky id was not reused: %q vs %q", id1, id2)
	}

	id3 := cache.stickyUserID(1, "other-fp")
	if id3 == id1 {
		t.Fatalf("different fingerprint reused same id: %q", id3)
	}

	if id := cache.stickyUserID(1, ""); id == "" || id == cache.stickyUserID(1, "") {
		t.Fatalf("empty fingerprint should generate fresh non-empty ids")
	}

	key := "1:expired"
	cache.entries[key] = sessionEntry{userID: "old", expiresAt: time.Now().Add(-time.Second)}
	cache.lastCleanupTime = time.Now().Add(-sessionCleanupInterval)
	newID := cache.stickyUserID(1, "expired")
	if newID == "old" {
		t.Fatalf("expired sticky id was reused")
	}
}

func TestSessionCacheCleanupAndDeleteOne(t *testing.T) {
	now := time.Now()
	cache := &sessionCache{
		entries: map[string]sessionEntry{
			"fresh":   {userID: "fresh", expiresAt: now.Add(time.Hour)},
			"expired": {userID: "expired", expiresAt: now.Add(-time.Hour)},
		},
		lastCleanupTime: now.Add(-sessionCleanupInterval),
	}
	cache.cleanupExpiredLocked(now)
	if _, ok := cache.entries["expired"]; ok {
		t.Fatalf("expired entry was not removed")
	}
	if _, ok := cache.entries["fresh"]; !ok {
		t.Fatalf("fresh entry was removed")
	}

	cache.entries["expired2"] = sessionEntry{userID: "expired2", expiresAt: now.Add(-time.Hour)}
	cache.deleteOneExpiredOrArbitraryLocked(now)
	if _, ok := cache.entries["expired2"]; ok {
		t.Fatalf("deleteOneExpiredOrArbitraryLocked did not prefer expired entry")
	}

	cache.entries = map[string]sessionEntry{"only": {userID: "only", expiresAt: now.Add(time.Hour)}}
	cache.deleteOneExpiredOrArbitraryLocked(now)
	if len(cache.entries) != 0 {
		t.Fatalf("arbitrary delete left entries: %#v", cache.entries)
	}
}
