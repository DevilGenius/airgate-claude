package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	utls "github.com/refraction-networking/utls"

	"github.com/DevilGenius/airgate-claude/backend/internal/gateway"
)

func TestExtensionID(t *testing.T) {
	tests := []struct {
		name string
		ext  utls.TLSExtension
		want uint16
	}{
		{"sni", &utls.SNIExtension{}, 0},
		{"status", &utls.StatusRequestExtension{}, 5},
		{"curves", &utls.SupportedCurvesExtension{}, 10},
		{"points", &utls.SupportedPointsExtension{}, 11},
		{"sigalgs", &utls.SignatureAlgorithmsExtension{}, 13},
		{"alpn", &utls.ALPNExtension{}, 16},
		{"sct", &utls.SCTExtension{}, 18},
		{"ems", &utls.ExtendedMasterSecretExtension{}, 23},
		{"ticket", &utls.SessionTicketExtension{}, 35},
		{"versions", &utls.SupportedVersionsExtension{}, 43},
		{"psk", &utls.PSKKeyExchangeModesExtension{}, 45},
		{"keyshare", &utls.KeyShareExtension{}, 51},
		{"ech", &utls.GREASEEncryptedClientHelloExtension{}, 65037},
		{"reneg", &utls.RenegotiationInfoExtension{}, 65281},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extensionID(tc.ext)
			if !ok || got != tc.want {
				t.Fatalf("extensionID(%T) = %d/%v, want %d/true", tc.ext, got, ok, tc.want)
			}
		})
	}
	if got, ok := extensionID(&utls.UtlsPaddingExtension{}); ok || got != 0 {
		t.Fatalf("unknown extension = %d/%v", got, ok)
	}
}

func TestSnapshotFromSpecAndBuildJA3(t *testing.T) {
	snap := snapshotFromSpec()
	if snap.CLIVersion != gateway.ClaudeCliVersion || snap.Runtime != "bun" || snap.RuntimeVersion != gateway.BunRuntimeVersion {
		t.Fatalf("snapshot runtime fields = %#v", snap)
	}
	if len(snap.CipherSuites) == 0 || len(snap.ExtensionIDs) == 0 || len(snap.Curves) == 0 || len(snap.SignatureAlgorithms) == 0 {
		t.Fatalf("snapshot missing TLS fields: %#v", snap)
	}
	if snap.JA3 == "" || len(snap.JA3Hash) != 32 {
		t.Fatalf("JA3/hash = %q/%q", snap.JA3, snap.JA3Hash)
	}

	ja3 := buildJA3(utls.VersionTLS13, []uint16{1, 2}, []uint16{0, 10}, []uint16{29})
	if ja3 != "771,1-2,0-10,29,0" {
		t.Fatalf("JA3 = %q", ja3)
	}
	legacy := buildJA3(0x0302, []uint16{1}, nil, nil)
	if legacy != "770,1,,,0" {
		t.Fatalf("legacy JA3 = %q", legacy)
	}
}

func TestLoadAndDiffSnapshots(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.json")
	snap := snapshotFromSpec()
	buf, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadSnapshot(path)
	if err != nil {
		t.Fatalf("loadSnapshot returned error: %v", err)
	}
	if diffs := diffSnapshots(snap, loaded); len(diffs) != 0 {
		t.Fatalf("identical snapshots diffed: %#v", diffs)
	}

	changed := loaded
	changed.CLIVersion = "different"
	changed.CipherSuites = append(changed.CipherSuites, 999)
	diffs := diffSnapshots(snap, changed)
	if len(diffs) != 2 {
		t.Fatalf("diff count = %d, want 2: %#v", len(diffs), diffs)
	}
	joined := strings.Join(diffs, "\n")
	if !strings.Contains(joined, "cli_version") || !strings.Contains(joined, "cipher_suites") {
		t.Fatalf("diffs missing changed fields: %#v", diffs)
	}

	if _, err := loadSnapshot(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatalf("missing snapshot should fail")
	}
	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSnapshot(badPath); err == nil {
		t.Fatalf("bad snapshot should fail")
	}
}

func TestRunCaptureAndVerifySuccess(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "capture.json")
	runCapture([]string{"--out", out})

	if info, err := os.Stat(out); err != nil || info.Size() == 0 {
		t.Fatalf("capture output stat = %v/%v", info, err)
	}
	runVerify([]string{"--baseline", out, "--sample", out})
	runVerify([]string{"--baseline", out})
}

func TestUsageWritesHelp(t *testing.T) {
	oldStderr := os.Stderr
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = writePipe
	t.Cleanup(func() { os.Stderr = oldStderr })

	usage()
	_ = writePipe.Close()
	out, err := io.ReadAll(readPipe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "fp capture") || !strings.Contains(string(out), "fp verify") {
		t.Fatalf("usage output = %q", out)
	}
}
