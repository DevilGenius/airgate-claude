package gateway

import "testing"

func TestGetHTTPClientTimeoutByStreamMode(t *testing.T) {
	nonStream := getHTTPClient(nil, nil, 1, "apikey", "", "", false)
	if nonStream.Timeout != httpTimeout {
		t.Fatalf("non-stream timeout = %s, want %s", nonStream.Timeout, httpTimeout)
	}

	stream := getHTTPClient(nil, nil, 1, "apikey", "", "", true)
	if stream.Timeout != 0 {
		t.Fatalf("stream timeout = %s, want 0", stream.Timeout)
	}
}
