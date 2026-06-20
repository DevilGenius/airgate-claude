package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/tidwall/gjson"
)

func TestHandleRequestOAuthSuccessPaths(t *testing.T) {
	server := newOAuthFixtureServer(t)
	defer server.Close()
	withOAuthEndpoints(t, server.URL, server.URL+"/oauth/token")

	oldStore := sessionStore
	sessionStore = &oauthSessionStore{sessions: map[string]*OAuthSession{}}
	t.Cleanup(func() { sessionStore = oldStore })
	sessionStore.Set("state", &OAuthSession{State: "state", CodeVerifier: "verifier"})

	g := &AnthropicGateway{logger: testLogger()}
	status, _, body, err := g.HandleRequest(context.Background(), "", "oauth/exchange", "", nil, []byte(`{"callback_url":"https://platform.claude.com/oauth/code/callback?code=auth-code&state=state"}`))
	if err != nil || status != http.StatusOK {
		t.Fatalf("oauth exchange callback status/error = %d/%v body=%s", status, err, body)
	}
	if gjson.GetBytes(body, "credentials.access_token").String() != "access-from-code" {
		t.Fatalf("oauth exchange body = %s", body)
	}

	status, _, body, err = g.HandleRequest(context.Background(), "", "oauth/exchange", "", nil, []byte(`{"callback_url":"{\"session_key\":\"session-key\"}"}`))
	if err != nil || status != http.StatusOK {
		t.Fatalf("oauth exchange session status/error = %d/%v body=%s", status, err, body)
	}
	if gjson.GetBytes(body, "account_name").String() != "user@example.test" {
		t.Fatalf("oauth exchange session body = %s", body)
	}

	status, _, body, err = g.HandleRequest(context.Background(), "", "oauth/refresh", "", nil, []byte(`{"refresh_token":"refresh"}`))
	if err != nil || status != http.StatusOK || gjson.GetBytes(body, "access_token").String() != "access-from-refresh" {
		t.Fatalf("oauth refresh status/error/body = %d/%v/%s", status, err, body)
	}

	status, _, body, err = g.HandleRequest(context.Background(), "", "console/cookie-auth", "", nil, []byte(`{"session_key":"session-key"}`))
	if err != nil || status != http.StatusOK || gjson.GetBytes(body, "account_name").String() != "user@example.test" {
		t.Fatalf("cookie auth status/error/body = %d/%v/%s", status, err, body)
	}

	status, _, body, err = g.HandleRequest(context.Background(), "", "console/batch-cookie-auth", "", nil, []byte(`{"session_keys":["session-key"]}`))
	if err != nil || status != http.StatusOK {
		t.Fatalf("batch auth status/error/body = %d/%v/%s", status, err, body)
	}
	var batch struct {
		Results []struct {
			Status string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &batch); err != nil || len(batch.Results) != 1 || batch.Results[0].Status != "ok" {
		t.Fatalf("batch auth decoded = %#v err=%v body=%s", batch, err, body)
	}
}
