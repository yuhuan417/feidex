package routing

import "testing"

func TestProfileModelSettingsAreBackendScoped(t *testing.T) {
	profile := &BotProfile{Model: "codex", ClaudeModel: "claude", SubagentModel: "codex-child", ClaudeSubagentModel: "claude-child"}
	if err := SetProfile(profile, "claude", Model, "new-claude"); err != nil {
		t.Fatal(err)
	}
	if profile.Model != "codex" || profile.ClaudeModel != "new-claude" {
		t.Fatalf("profile = %+v", profile)
	}
	if err := SetProfile(profile, "codex", SubagentModel, "new-codex-child"); err != nil {
		t.Fatal(err)
	}
	if profile.SubagentModel != "new-codex-child" || profile.ClaudeSubagentModel != "claude-child" {
		t.Fatalf("profile = %+v", profile)
	}
	if err := SetProfile(profile, "claude", PlanModel, "unused"); err != nil {
		t.Fatal(err)
	}
	if profile.PlanModel != "" {
		t.Fatal("Claude changed Codex Plan settings")
	}
}

func TestInvalidSettingDoesNotMutateAggregate(t *testing.T) {
	binding := AgentBinding{ModelOverride: "old"}
	profile := BotProfile{Model: "old"}
	if SetBinding(&binding, "invalid", "new") == nil || binding.ModelOverride != "old" {
		t.Fatal("invalid binding setting accepted")
	}
	if SetProfile(&profile, "codex", "invalid", "new") == nil || profile.Model != "old" {
		t.Fatal("invalid profile setting accepted")
	}
}
