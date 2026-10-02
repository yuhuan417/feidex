package modelconfig

import "testing"

func TestResolveUsesChatScopeBeforeFrontendAndGlobalValues(t *testing.T) {
	got := Resolve(BackendCodex, Sources{
		Session: SessionValues{Model: "session-model"},
		Binding: ScopeValues{Model: "group-model"},
		Profile: ProfileValues{Model: "profile-model"},
		Global:  GlobalValues{Model: "global-model"},
	})
	if got.Model != "session-model" {
		t.Fatalf("model = %q, want session-model", got.Model)
	}

	got = Resolve(BackendCodex, Sources{
		Binding: ScopeValues{Model: "group-model"},
		Profile: ProfileValues{Model: "profile-model"},
		Global:  GlobalValues{Model: "global-model"},
	})
	if got.Model != "group-model" {
		t.Fatalf("model without session override = %q, want group-model", got.Model)
	}
}

func TestResolveCodexPlanUsesActiveModeWithoutChangingSavedMainModel(t *testing.T) {
	got := Resolve(BackendCodex, Sources{
		Global: GlobalValues{Model: "main", PlanModel: "plan", PlanEffort: "high"},
		Active: &ActiveCollaboration{Mode: "plan", Model: "active-plan", PresetEffort: "medium"},
	})
	if got.Model != "main" || got.PlanModel != "plan" || got.PlanEffort != "high" {
		t.Fatalf("snapshot = %+v", got)
	}
	model, effort := TurnSettings(got)
	if model != "plan" || effort != "high" {
		t.Fatalf("turn settings = %q/%q, want plan/high", model, effort)
	}
}

func TestResolveClaudeUsesClaudeScopes(t *testing.T) {
	got := Resolve(BackendClaude, Sources{
		Session: SessionValues{Model: "session"},
		Binding: ScopeValues{Model: "group", Effort: "high"},
		Profile: ProfileValues{ClaudeModel: "profile", Effort: "medium", ClaudeSmallModel: "small-profile"},
		Global:  GlobalValues{ClaudeModel: "global", ClaudeEffort: "low", ClaudeSmallModel: "small-global", ClaudeSubagent: "subagent"},
	})
	if got.Model != "session" || got.Effort != "high" || got.SmallModel != "small-profile" || got.SubagentModel != "subagent" {
		t.Fatalf("snapshot = %+v", got)
	}
}
