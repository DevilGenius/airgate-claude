package gateway

import (
	"slices"
	"testing"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestPluginMetadata(t *testing.T) {
	if deps := PluginDependencies(); len(deps) != 0 {
		t.Fatalf("PluginDependencies = %#v, want empty", deps)
	}

	info := BuildPluginInfo()
	if info.ID != PluginID || info.Name != PluginDisplayName || info.Version != PluginVersion || info.Type != sdk.PluginTypeGateway {
		t.Fatalf("unexpected plugin info: %#v", info)
	}
	if len(info.AccountTypes) != 2 {
		t.Fatalf("account type count = %d", len(info.AccountTypes))
	}
	keys := []string{info.AccountTypes[0].Key, info.AccountTypes[1].Key}
	if !slices.Contains(keys, "apikey") || !slices.Contains(keys, "oauth") {
		t.Fatalf("account type keys = %#v", keys)
	}
	if len(info.FrontendWidgets) != 6 {
		t.Fatalf("frontend widget count = %d", len(info.FrontendWidgets))
	}

	routes := PluginRouteDefinitions()
	if len(routes) != 3 {
		t.Fatalf("route count = %d", len(routes))
	}
	if routes[0].Method != httpPost || routes[0].Path != "/v1/messages" {
		t.Fatalf("first route = %#v", routes[0])
	}
}

const httpPost = "POST"
