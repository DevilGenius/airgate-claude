package gateway

import (
	"encoding/json"
	"testing"
)

func TestSanitizeBodyNoopCases(t *testing.T) {
	for _, body := range [][]byte{
		nil,
		[]byte(`{}`),
		[]byte(`{"messages":"not-array"}`),
		[]byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	} {
		got := sanitizeBody(body)
		if string(got) != string(body) {
			t.Fatalf("sanitizeBody(%s) = %s", body, got)
		}
	}
}

func TestSanitizeBodyDropsEmptyStringMessagesAndEmptyArrays(t *testing.T) {
	in := []byte(`{"messages":[{"role":"user","content":""},{"role":"user","content":[{"type":"text","text":""}]},{"role":"assistant","content":[{"type":"thinking","thinking":"keep"}]}]}`)
	out := sanitizeBody(in)

	var parsed struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("sanitized body is invalid JSON: %v\n%s", err, out)
	}
	if len(parsed.Messages) != 1 {
		t.Fatalf("message count = %d, want 1; body=%s", len(parsed.Messages), out)
	}
	if parsed.Messages[0].Role != "assistant" {
		t.Fatalf("remaining role = %q", parsed.Messages[0].Role)
	}
}

func TestFilterContentBlocksThinkingRules(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"thinking","thinking":"drop"},{"type":"text","text":"hi"}]},{"role":"assistant","content":[{"type":"text","text":"ok"},{"type":"redacted_thinking","data":"keep"}]}]}`)
	out := sanitizeBody(body)
	got := string(out)

	if contains(got, `"thinking":"drop"`) {
		t.Fatalf("non-assistant thinking block survived: %s", got)
	}
	if !contains(got, `"text":"hi"`) {
		t.Fatalf("user text was removed: %s", got)
	}
	if !contains(got, `"redacted_thinking"`) {
		t.Fatalf("assistant final redacted thinking block was removed: %s", got)
	}
}
