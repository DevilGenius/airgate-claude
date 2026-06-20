package gateway

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestTokenRefreshErrorWrapping(t *testing.T) {
	if newTokenRefreshError(nil, true) != nil {
		t.Fatalf("nil error should stay nil")
	}
	base := errors.New("invalid_grant")
	err := newTokenRefreshError(base, true)
	if err.Error() != base.Error() || !errors.Is(err, base) {
		t.Fatalf("wrapped error mismatch: %v", err)
	}
	if !isAccountTokenRefreshError(err) {
		t.Fatalf("account refresh error was not detected")
	}
	if isAccountTokenRefreshError(newTokenRefreshError(base, false)) {
		t.Fatalf("transient refresh error detected as account fault")
	}
}

func TestTokenManagerStateLifecycle(t *testing.T) {
	m := newTokenManager(&AnthropicGateway{}, slog.New(slog.NewTextHandler(ioDiscard{}, nil)))
	state := m.getState(1)
	if state == nil || len(m.locks) != 1 {
		t.Fatalf("getState did not create state")
	}
	if again := m.getState(1); again != state {
		t.Fatalf("getState did not reuse state")
	}

	old := &accountRefreshState{lastSeenAt: time.Now().Add(-tokenStateIdleTTL - time.Hour)}
	m.locks[2] = old
	m.lastCleanupTime = time.Now().Add(-tokenStateCleanupInterval - time.Second)
	m.cleanupLocked(time.Now())
	if _, ok := m.locks[2]; ok {
		t.Fatalf("cleanupLocked left idle state")
	}

	m.locks = map[int64]*accountRefreshState{
		1: {lastSeenAt: time.Now().Add(-time.Hour)},
		2: {lastSeenAt: time.Now()},
	}
	m.deleteOldestLocked()
	if _, ok := m.locks[1]; ok {
		t.Fatalf("deleteOldestLocked did not remove oldest")
	}
}

func TestAccountRefreshStateAndCredentialUpdate(t *testing.T) {
	state := &accountRefreshState{}
	if got := state.freshCredentials(tokenRefreshSkew); got != nil {
		t.Fatalf("empty state fresh credentials = %#v", got)
	}

	expiresAt := time.Now().Add(time.Hour)
	state.rememberToken("access", "refresh", expiresAt, expiresAt.Format(time.RFC3339))
	fresh := state.freshCredentials(tokenRefreshSkew)
	if fresh["access_token"] != "access" || fresh["refresh_token"] != "refresh" || fresh["expires_at"] == "" {
		t.Fatalf("fresh credentials = %#v", fresh)
	}

	account := &sdk.Account{Credentials: map[string]string{"access_token": "old"}}
	if !applyCredentialUpdate(account, map[string]string{"access_token": "access", "empty": ""}) {
		t.Fatalf("applyCredentialUpdate should report change")
	}
	if account.Credentials["access_token"] != "access" {
		t.Fatalf("access token was not updated")
	}
	if _, ok := account.Credentials["empty"]; ok {
		t.Fatalf("empty value should not be written")
	}
	if applyCredentialUpdate(account, map[string]string{"access_token": "access"}) {
		t.Fatalf("unchanged credentials should report false")
	}
	if applyCredentialUpdate(nil, map[string]string{"x": "y"}) {
		t.Fatalf("nil account should report false")
	}
}

func TestEnsureValidTokenEarlyReturnsAndSessionKeyError(t *testing.T) {
	m := newTokenManager(&AnthropicGateway{logger: testLogger()}, testLogger())

	noRefresh := &sdk.Account{ID: 1, Type: "oauth", Credentials: map[string]string{"access_token": "tok"}}
	if updated, err := m.ensureValidToken(context.Background(), noRefresh); err != nil || updated != nil {
		t.Fatalf("no refresh token updated/error = %#v/%v", updated, err)
	}

	badExpires := &sdk.Account{ID: 2, Type: "oauth", Credentials: map[string]string{"refresh_token": "rt", "expires_at": "not-time"}}
	if updated, err := m.ensureValidToken(context.Background(), badExpires); err != nil || updated != nil {
		t.Fatalf("bad expires updated/error = %#v/%v", updated, err)
	}

	future := &sdk.Account{ID: 3, Type: "oauth", Credentials: map[string]string{"refresh_token": "rt", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)}}
	if updated, err := m.ensureValidToken(context.Background(), future); err != nil || updated != nil {
		t.Fatalf("future expires updated/error = %#v/%v", updated, err)
	}

	missingSession := &sdk.Account{ID: 4, Type: "session_key", Credentials: map[string]string{}}
	if _, err := m.ensureValidToken(context.Background(), missingSession); err == nil || !isAccountTokenRefreshError(err) {
		t.Fatalf("missing session_key error = %v", err)
	}
}

func TestDoRefreshReusesFreshStateAndCooldown(t *testing.T) {
	m := newTokenManager(&AnthropicGateway{logger: testLogger()}, testLogger())
	expiresAt := time.Now().Add(time.Hour)
	state := &accountRefreshState{}
	state.rememberToken("fresh-access", "fresh-refresh", expiresAt, expiresAt.Format(time.RFC3339))
	m.locks[9] = state

	account := &sdk.Account{ID: 9, Type: "oauth", Credentials: map[string]string{"refresh_token": "old", "expires_at": time.Now().Add(-time.Hour).Format(time.RFC3339)}}
	updated, err := m.doRefresh(context.Background(), account)
	if err != nil {
		t.Fatalf("doRefresh reuse returned error: %v", err)
	}
	if updated["access_token"] != "fresh-access" || account.Credentials["access_token"] != "fresh-access" {
		t.Fatalf("fresh state was not reused: updated=%#v account=%#v", updated, account.Credentials)
	}

	nonRetryable := errors.New("invalid_grant: revoked")
	cooldownState := &accountRefreshState{lastError: nonRetryable, lastErrorAt: time.Now(), lastSeenAt: time.Now()}
	m.locks[10] = cooldownState
	cooldownAccount := &sdk.Account{ID: 10, Type: "oauth", Credentials: map[string]string{"refresh_token": "rt", "expires_at": time.Now().Add(-time.Hour).Format(time.RFC3339)}}
	if _, err := m.doRefresh(context.Background(), cooldownAccount); err == nil || !isAccountTokenRefreshError(err) {
		t.Fatalf("cooldown error = %v", err)
	}
}

func TestIsNonRetryableRefreshError(t *testing.T) {
	if isNonRetryableRefreshError(nil) {
		t.Fatalf("nil should be retryable")
	}
	for _, msg := range []string{"invalid_grant", "invalid_client", "unauthorized_client", "access_denied", "missing_project_id", "no refresh token available"} {
		if !isNonRetryableRefreshError(errors.New("server: " + msg)) {
			t.Fatalf("%q should be non-retryable", msg)
		}
	}
	if isNonRetryableRefreshError(errors.New("temporary network failure")) {
		t.Fatalf("temporary error should be retryable")
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
