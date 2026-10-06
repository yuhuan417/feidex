package routing

import (
	"testing"

	"feidex/internal/domain/modelconfig"
)

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
	if err := SetProfile(profile, "claude", PlanModel, "unused"); err == nil {
		t.Fatal("expected Claude plan write to be rejected instead of dropped silently")
	}
	if profile.PlanModel != "" {
		t.Fatal("Claude changed Codex Plan settings")
	}
	if err := SetProfile(profile, "codex", SmallModel, "unused"); err == nil {
		t.Fatal("expected Codex small-model write to be rejected instead of dropped silently")
	}
	if profile.ClaudeSmallModel != "" {
		t.Fatal("Codex changed Claude small-model setting")
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

func TestBindingFieldRoundTrip(t *testing.T) {
	settings := []Setting{Model, Effort, PlanModel, PlanEffort, ReviewModel, SubagentModel, SubagentEffort, SmallModel, ServiceTier, Workspace, Sandbox, ApprovalPolicy, MultiAgent, Permissions}
	binding := &AgentBinding{}
	for _, setting := range settings {
		field := BindingField(binding, setting)
		if field == nil {
			t.Fatalf("binding does not persist setting %q", setting)
		}
		*field = "v-" + string(setting)
		if got := BindingValue(binding, setting); got != "v-"+string(setting) {
			t.Fatalf("BindingValue(%q) = %q", setting, got)
		}
	}
	if BindingField(binding, "unknown") != nil || BindingValue(nil, Model) != "" {
		t.Fatal("unexpected binding field lookup result")
	}
}

func TestProfileFieldBackendSelection(t *testing.T) {
	profile := &BotProfile{}
	if err := SetProfile(profile, "claude", SmallModel, "haiku-x"); err != nil {
		t.Fatal(err)
	}
	if profile.ClaudeSmallModel != "haiku-x" || ProfileValue(profile, "claude", SmallModel) != "haiku-x" {
		t.Fatalf("profile = %+v", profile)
	}
	if err := SetProfile(profile, "codex", Permissions, "acceptEdits"); err != nil {
		t.Fatal(err)
	}
	if profile.ClaudePermissionMode != "acceptEdits" {
		t.Fatalf("profile = %+v", profile)
	}
	for _, setting := range []Setting{PlanModel, PlanEffort, ReviewModel, SubagentEffort} {
		if ProfileField(profile, setting, "claude") != nil {
			t.Fatalf("claude should not persist %q on the profile tier", setting)
		}
	}
	if ProfileValue(profile, "codex", SmallModel) != "" {
		t.Fatal("codex profile must not expose a small-model value")
	}
}

func TestGlobalFieldBackendSelection(t *testing.T) {
	values := &modelconfig.GlobalValues{}
	if err := defaultsSet(values, "claude", Model, "claude-main"); err != nil {
		t.Fatal(err)
	}
	if values.ClaudeModel != "claude-main" || values.Model != "" {
		t.Fatalf("values = %+v", values)
	}
	if err := defaultsSet(values, "codex", SubagentModel, "codex-child"); err != nil {
		t.Fatal(err)
	}
	if values.SubagentModel != "codex-child" || values.ClaudeSubagent != "" {
		t.Fatalf("values = %+v", values)
	}
	for _, setting := range []Setting{PlanModel, PlanEffort, ReviewModel, SubagentEffort} {
		if GlobalField(values, setting, "claude") != nil {
			t.Fatalf("claude should not persist %q globally", setting)
		}
	}
	if GlobalField(values, SmallModel, "codex") != nil {
		t.Fatal("codex should not persist small model globally")
	}
	if GlobalField(nil, Model, "codex") != nil {
		t.Fatal("nil global values must not yield a field pointer")
	}
}

// defaultsSet writes one global field through the same dual-write shape the
// defaults service uses.
func defaultsSet(values *modelconfig.GlobalValues, backend string, setting Setting, value string) error {
	field := GlobalField(values, setting, backend)
	if field == nil {
		return &unsupportedSettingError{backend: backend, setting: setting}
	}
	*field = value
	return nil
}

type unsupportedSettingError struct {
	backend string
	setting Setting
}

func (e *unsupportedSettingError) Error() string { return "unsupported setting" }
