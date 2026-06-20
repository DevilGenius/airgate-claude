package gateway

import (
	"testing"
	"time"
)

func TestNewAccountUsageWindow(t *testing.T) {
	now := time.Date(2026, 6, 20, 1, 2, 3, 0, time.UTC)
	reset := now.Add(90 * time.Second)

	window := newAccountUsageWindow("5h", "5h", "Current", "slot", "group", 42.5, &reset, now)
	if window.Key != "5h" || window.Label != "5h" || window.DisplayLabel != "Current" || window.Slot != "slot" || window.Group != "group" {
		t.Fatalf("identity fields not preserved: %#v", window)
	}
	if window.UsedPercent != 42.5 {
		t.Fatalf("used percent = %v", window.UsedPercent)
	}
	if window.UpdatedAt != now.Format(time.RFC3339) || window.ResetAt != reset.Format(time.RFC3339) {
		t.Fatalf("timestamps = updated %q reset %q", window.UpdatedAt, window.ResetAt)
	}
	if window.ResetSeconds != 90 {
		t.Fatalf("reset seconds = %d, want 90", window.ResetSeconds)
	}

	past := now.Add(-time.Second)
	pastWindow := newAccountUsageWindow("7d", "7d", "", "", "", 1, &past, now)
	if pastWindow.ResetSeconds != 0 {
		t.Fatalf("past reset should not set reset seconds: %#v", pastWindow)
	}

	noReset := newAccountUsageWindow("x", "X", "", "", "", 0, nil, now)
	if noReset.ResetAt != "" || noReset.ResetSeconds != 0 {
		t.Fatalf("nil reset produced reset fields: %#v", noReset)
	}
}
