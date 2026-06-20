package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAssetsFromDir(t *testing.T) {
	if got := loadAssetsFromDir(filepath.Join(t.TempDir(), "missing")); got != nil {
		t.Fatalf("missing dir assets = %#v", got)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "style.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	assets := loadAssetsFromDir(root)
	if string(assets["index.js"]) != "console.log(1)" || string(assets["nested/style.css"]) != "body{}" {
		t.Fatalf("assets = %#v", assets)
	}
}

func TestLoadDevAndEmbeddedWebAssets(t *testing.T) {
	assets := (&AnthropicGateway{logger: testLogger()}).GetWebAssets()
	if len(assets) == 0 {
		t.Fatalf("GetWebAssets returned no assets")
	}
	if _, ok := assets["index.js"]; !ok {
		t.Fatalf("GetWebAssets missing index.js: keys=%#v", assets)
	}
}

func TestLoadDevWebAssetsFromWorkingDirectory(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	if err := os.MkdirAll(filepath.Join(root, "web", "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "web", "dist", "index.js"), []byte("dev"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	assets := loadDevWebAssets()
	if string(assets["index.js"]) != "dev" {
		t.Fatalf("dev assets = %#v", assets)
	}
}
