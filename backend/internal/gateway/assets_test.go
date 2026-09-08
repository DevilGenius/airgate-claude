package gateway

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadAssetsFromDir(t *testing.T) {
	before := (&AnthropicGateway{}).GetWebAssets()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web", "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "web", "dist", "index.js"), []byte("wrong project"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if after := (&AnthropicGateway{}).GetWebAssets(); !reflect.DeepEqual(before, after) {
		t.Fatal("working directory changed embedded assets")
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
	before := (&AnthropicGateway{}).GetWebAssets()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web", "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "web", "dist", "index.js"), []byte("wrong project"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if after := (&AnthropicGateway{}).GetWebAssets(); !reflect.DeepEqual(before, after) {
		t.Fatal("working directory changed embedded assets")
	}
}
