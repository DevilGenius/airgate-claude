package gateway

import (
	"encoding/json"
	"slices"
	"testing"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestNormalizeAndLookupModelSpec(t *testing.T) {
	if got := NormalizeModelID("claude-sonnet-4-5"); got != "claude-sonnet-4-5-20250929" {
		t.Fatalf("NormalizeModelID short = %q", got)
	}
	if got := NormalizeModelID("custom-model"); got != "custom-model" {
		t.Fatalf("NormalizeModelID unchanged = %q", got)
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"exact", "claude-opus-4-7", "claude-opus-4-7"},
		{"normalized", "claude-haiku-4-5", "claude-haiku-4-5-20251001"},
		{"prefix", "claude-sonnet-4-6-extra", "claude-sonnet-4-6"},
		{"keyword sonnet", "vendor-sonnet-next", "claude-sonnet-4-6"},
		{"keyword haiku", "vendor-haiku-next", "claude-haiku-4-5-20251001"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, spec := LookupModelSpec(tc.in)
			if got != tc.want {
				t.Fatalf("LookupModelSpec(%q) id = %q, want %q", tc.in, got, tc.want)
			}
			if spec.Name == "" {
				t.Fatalf("LookupModelSpec(%q) returned empty spec", tc.in)
			}
		})
	}
}

func TestAllPricingAndModelSpecs(t *testing.T) {
	pricing := AllPricingSpecs()
	if len(pricing) != len(modelRegistry) {
		t.Fatalf("pricing len = %d, want %d", len(pricing), len(modelRegistry))
	}
	if !slices.IsSortedFunc(pricing, func(a, b NamedSpec) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	}) {
		t.Fatalf("AllPricingSpecs is not sorted: %#v", pricing)
	}

	models := AllModelSpecs()
	if len(models) != len(pricing) {
		t.Fatalf("models len = %d, want %d", len(models), len(pricing))
	}
	for _, model := range models {
		if model.ID == "" || model.Name == "" || model.ContextWindow == 0 || model.MaxOutputTokens == 0 {
			t.Fatalf("incomplete model info: %#v", model)
		}
		if !slices.Contains(model.Capabilities, sdk.ModelCapChat) || !slices.Contains(model.Capabilities, sdk.ModelCapReasoning) {
			t.Fatalf("model capabilities missing chat/reasoning: %#v", model)
		}
	}
}

func TestUsageMetadataAndCostHelpers(t *testing.T) {
	if usageTotalTokens(nil) != 0 {
		t.Fatalf("usageTotalTokens(nil) should be 0")
	}
	if usageMetadataFloat(nil, "x") != 0 {
		t.Fatalf("usageMetadataFloat(nil) should be 0")
	}

	usage := &sdk.Usage{Model: "claude-opus-4-8"}
	setUsageMetadata(usage, "blank", " ")
	if _, ok := usage.Metadata["blank"]; ok {
		t.Fatalf("blank metadata should be ignored")
	}
	setUsageMetadataInt(usage, "int", 12)
	setUsageMetadataInt(usage, "zero", 0)
	setUsageMetadataFloat(usage, "float", 1.25)
	setUsageMetadataFloat(usage, "negative", -1)
	usage.Metadata["bad"] = "not-a-float"

	if got := usageMetadataFloat(usage, "int"); got != 12 {
		t.Fatalf("metadata int = %v", got)
	}
	if got := usageMetadataFloat(usage, "float"); got != 1.25 {
		t.Fatalf("metadata float = %v", got)
	}
	if got := usageMetadataFloat(usage, "bad"); got != 0 {
		t.Fatalf("bad metadata float = %v", got)
	}
	if _, ok := usage.Metadata["zero"]; ok {
		t.Fatalf("zero metadata should be ignored")
	}
	if _, ok := usage.Metadata["negative"]; ok {
		t.Fatalf("negative metadata should be ignored")
	}

	setUsageTokens(usage, tokenUsage{
		inputTokens:           1000,
		outputTokens:          2000,
		cachedInputTokens:     3000,
		cacheCreationTokens:   900,
		cacheCreation5mTokens: 400,
		cacheCreation1hTokens: 500,
		reasoningOutputTokens: 600,
	})
	fillUsageCost(usage)

	if usage.Currency != usageCurrencyUSD {
		t.Fatalf("currency = %q", usage.Currency)
	}
	if usage.InputCost <= 0 || usage.OutputCost <= 0 || usage.CachedInputCost <= 0 || usage.CacheCreationCost <= 0 {
		t.Fatalf("expected all cost components to be populated: %#v", usage)
	}
	if usage.AccountCost != usage.InputCost+usage.OutputCost+usage.CachedInputCost+usage.CacheCreationCost {
		t.Fatalf("account cost was not recomputed: %#v", usage)
	}

	empty := &sdk.Usage{}
	fillUsageCost(empty)
	if empty.AccountCost != 0 {
		t.Fatalf("empty usage should not be billed: %#v", empty)
	}
}

func TestTokenCostAndFallbackBilling(t *testing.T) {
	if tokenCost(0, 1) != 0 || tokenCost(10, 0) != 0 {
		t.Fatalf("zero token or price should cost 0")
	}
	if got := tokenCost(1_000_000, 3.5); got != 3.5 {
		t.Fatalf("tokenCost = %v, want 3.5", got)
	}

	usage := &sdk.Usage{Model: "unknown-future-model", InputTokens: 1_000_000}
	fillUsageCost(usage)
	if usage.InputPrice != fallbackSpec.InputPrice || usage.InputCost != fallbackSpec.InputPrice {
		t.Fatalf("fallback billing not applied: %#v", usage)
	}
}

func TestBuildModelsResponse(t *testing.T) {
	var payload struct {
		Data    []claudeModelListEntry `json:"data"`
		HasMore bool                   `json:"has_more"`
		FirstID string                 `json:"first_id"`
		LastID  string                 `json:"last_id"`
	}
	if err := json.Unmarshal(buildModelsResponse(), &payload); err != nil {
		t.Fatalf("buildModelsResponse produced invalid JSON: %v", err)
	}
	if payload.HasMore {
		t.Fatalf("has_more = true, want false")
	}
	if payload.FirstID != defaultModelList[0].ID || payload.LastID != defaultModelList[len(defaultModelList)-1].ID {
		t.Fatalf("first/last ids = %q/%q", payload.FirstID, payload.LastID)
	}
	if len(payload.Data) != len(defaultModelList) {
		t.Fatalf("model count = %d, want %d", len(payload.Data), len(defaultModelList))
	}
}
