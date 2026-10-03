package modelconfig

import (
	"testing"

	"feidex/internal/domain/modelconfig"
)

func TestCatalogPolicyResolvesDefaultAndEffort(t *testing.T) {
	result := modelconfig.ModelListResult{Data: []modelconfig.ModelListEntry{
		{ID: "first", Model: "first"},
		{ID: "default", Model: "default", IsDefault: true, SupportedReasoningEfforts: []modelconfig.ModelReasoningEffortEntry{{ReasoningEffort: "high"}}},
	}}
	if got := FindModelEntry(result, "missing"); got == nil || got.ID != "default" {
		t.Fatalf("fallback model = %#v", got)
	}
	if got := FindModelEntry(result, "first"); got == nil || got.ID != "first" {
		t.Fatalf("lookup model = %#v", got)
	}
	if !ModelSupportsEffort(FindModelEntry(result, "default"), "high") || ModelSupportsEffort(FindModelEntry(result, "default"), "low") {
		t.Fatal("effort support policy mismatch")
	}
}
