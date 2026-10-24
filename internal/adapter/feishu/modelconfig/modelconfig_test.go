package modelconfig

import (
	"testing"

	catalog "feidex/internal/domain/modelconfig"

	"feidex/internal/config"
)

func TestMergeCodexModelOptionsAppendsConfiguredCandidates(t *testing.T) {
	catalogResult := catalog.ModelListResult{Data: []catalog.ModelListEntry{{
		ID:          "gpt-5",
		Model:       "gpt-5",
		DisplayName: "GPT-5",
		IsDefault:   true,
		SupportedReasoningEfforts: []catalog.ModelReasoningEffortEntry{
			{ReasoningEffort: "low"},
			{ReasoningEffort: "medium"},
		},
	}}}
	cfg := &config.Config{}
	cfg.Codex.ModelOptions = []string{"claude-opus-4.6", " gpt-5 ", "gemini-3-pro", " "}

	merged := MergeCodexModelOptions(catalogResult, cfg)
	if len(merged.Data) != 3 {
		t.Fatalf("merged catalog size = %d, want 3: %+v", len(merged.Data), merged.Data)
	}
	if merged.Data[0].ID != "gpt-5" || !merged.Data[0].IsDefault {
		t.Fatalf("first merged entry = %+v, want the untouched catalog default", merged.Data[0])
	}
	if merged.Data[1].ID != "claude-opus-4.6" || merged.Data[2].ID != "gemini-3-pro" {
		t.Fatalf("merged entries = %+v, want configured candidates appended in order", merged.Data[1:])
	}
	for _, entry := range merged.Data[1:] {
		if entry.Model != entry.ID || entry.DisplayName != entry.ID {
			t.Fatalf("synthesized entry = %+v, want id mirrored into model and display name", entry)
		}
		if len(entry.SupportedReasoningEfforts) != 0 || entry.IsDefault {
			t.Fatalf("synthesized entry = %+v, want no effort metadata and no default marker", entry)
		}
	}
}

func TestMergeCodexModelOptionsWithoutOptionsReturnsCatalog(t *testing.T) {
	catalogResult := catalog.ModelListResult{Data: []catalog.ModelListEntry{{ID: "gpt-5", DisplayName: "GPT-5"}}}
	merged := MergeCodexModelOptions(catalogResult, &config.Config{})
	if len(merged.Data) != 1 || merged.Data[0].ID != "gpt-5" {
		t.Fatalf("merged catalog = %+v, want the catalog unchanged", merged.Data)
	}
	merged = MergeCodexModelOptions(catalogResult, nil)
	if len(merged.Data) != 1 || merged.Data[0].ID != "gpt-5" {
		t.Fatalf("merged catalog = %+v, want the catalog unchanged for nil config", merged.Data)
	}
}

func TestEffectiveConfiguredModelResolvesConfiguredCandidate(t *testing.T) {
	catalogResult := catalog.ModelListResult{Data: []catalog.ModelListEntry{{
		ID:                     "gpt-5",
		DisplayName:            "GPT-5",
		DefaultReasoningEffort: "medium",
		SupportedReasoningEfforts: []catalog.ModelReasoningEffortEntry{
			{ReasoningEffort: "medium"},
		},
		IsDefault: true,
	}}}
	cfg := &config.Config{}
	cfg.Codex.ModelOptions = []string{"claude-opus-4.6"}
	merged := MergeCodexModelOptions(catalogResult, cfg)

	cfg.Codex.Model = "claude-opus-4.6"
	model, effort := EffectiveConfiguredModelAndEffort(cfg, merged)
	if model == nil || model.ID != "claude-opus-4.6" {
		t.Fatalf("effective model = %+v, want the configured candidate entry", model)
	}
	if effort != "" {
		t.Fatalf("effective effort = %q, want empty because candidates carry no effort metadata", effort)
	}

	// A configured effort that the candidate cannot advertise is reset for
	// display and persistence, matching the catalog-model policy.
	cfg.Codex.ReasoningEffort = "high"
	model, effort = EffectiveConfiguredModelAndEffort(cfg, merged)
	if model == nil || model.ID != "claude-opus-4.6" || effort != "" {
		t.Fatalf("effective model/effort = %+v/%q, want candidate with effort reset", model, effort)
	}

	// Catalog models keep their effort defaults after the merge.
	cfg.Codex.Model, cfg.Codex.ReasoningEffort = "gpt-5", ""
	model, effort = EffectiveConfiguredModelAndEffort(cfg, merged)
	if model == nil || model.ID != "gpt-5" || effort != "medium" {
		t.Fatalf("effective model/effort = %+v/%q, want the catalog default effort", model, effort)
	}
}
