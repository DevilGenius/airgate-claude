package gateway

import (
	"context"
	"github.com/DevilGenius/airgate-sdk/devkit/testhost"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
	"testing"
	"time"
)

func TestSharedClaudeIdentityAndOAuthAcrossGenerations(t *testing.T) {
	host := &testhost.State{}
	a, b := &sdk.RuntimeStateClient{Host: host}, &sdk.RuntimeStateClient{Host: host}
	old, newer := &sessionCache{shared: a}, &sessionCache{shared: b}
	id := old.stickyUserID(1, "conversation")
	if id == "" || newer.stickyUserID(1, "conversation") != id {
		t.Fatal("session identity changed at cutover")
	}
	sa, sb := &oauthSessionStore{shared: a}, &oauthSessionStore{shared: b}
	if err := sa.Set("state", &OAuthSession{State: "state", CodeVerifier: "test verifier", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	session, ok, err := sb.consume(context.Background(), "state")
	if err != nil || !ok || session.CodeVerifier != "test verifier" {
		t.Fatalf("OAuth state lost: %+v %t %v", session, ok, err)
	}
	if _, ok, err := sa.consume(context.Background(), "state"); err != nil || ok {
		t.Fatal("OAuth consumed twice", err)
	}
}
