package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestGatewayLifecycleAndMetadataMethods(t *testing.T) {
	g := &AnthropicGateway{logger: testLogger()}
	if err := g.Init(nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if g.stdPool == nil || g.fpPool == nil || g.clientPool == nil || g.registry == nil || g.sidecar == nil || g.tokenMgr == nil {
		t.Fatalf("Init did not initialize all dependencies: %#v", g)
	}
	if g.Info().ID != PluginID {
		t.Fatalf("Info ID = %q", g.Info().ID)
	}
	if g.Platform() != PluginPlatform {
		t.Fatalf("Platform = %q", g.Platform())
	}
	if len(g.Models()) == 0 {
		t.Fatalf("Models returned empty list")
	}
	if len(g.Routes()) == 0 {
		t.Fatalf("Routes returned empty list")
	}
	if err := g.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := g.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
}

func TestGatewayHandleWebSocketNotSupported(t *testing.T) {
	g := &AnthropicGateway{}
	if _, err := g.HandleWebSocket(context.Background(), nil); err != sdk.ErrNotSupported {
		t.Fatalf("HandleWebSocket error = %v, want ErrNotSupported", err)
	}
}

func TestGatewayForwardModelsAndUnknownAccount(t *testing.T) {
	g := newTestGateway(t)

	rec := httptest.NewRecorder()
	outcome, err := g.Forward(context.Background(), &sdk.ForwardRequest{
		Headers: http.Header{"X-Original-Path": []string{"/v1/models"}},
		Writer:  rec,
	})
	if err != nil {
		t.Fatalf("Forward models returned error: %v", err)
	}
	if outcome.Kind != sdk.OutcomeSuccess || rec.Code != http.StatusOK {
		t.Fatalf("models outcome/recorder = %#v/%d", outcome, rec.Code)
	}

	outcome, err = g.Forward(context.Background(), &sdk.ForwardRequest{
		Account: &sdk.Account{ID: 1, Type: "mystery", Credentials: map[string]string{}},
		Headers: http.Header{},
		Body:    []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	})
	if err == nil {
		t.Fatalf("Forward unknown account type returned nil error")
	}
	if outcome.Kind != sdk.OutcomeAccountDead {
		t.Fatalf("unknown account outcome = %#v", outcome)
	}
}
