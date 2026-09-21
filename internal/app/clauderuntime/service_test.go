package clauderuntime

import "testing"

func TestWithClaudeModelEnv(t *testing.T) {
	env := withClaudeModelEnv([]string{
		"PATH=/usr/bin",
		"ANTHROPIC_MODEL=old-model",
		"ANTHROPIC_MODEL=duplicate-old-model",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=old-small-model",
	}, " deepseek-flash[1m] ", " deepseek-flash ", " subagent-model ")

	got := make(map[string][]string)
	for _, entry := range env {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				got[entry[:i]] = append(got[entry[:i]], entry[i+1:])
				break
			}
		}
	}

	want := map[string]string{
		"PATH":                           "/usr/bin",
		"ANTHROPIC_MODEL":                "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "deepseek-flash",
		"CLAUDE_CODE_SUBAGENT_MODEL":     "subagent-model",
	}
	for key, value := range want {
		values := got[key]
		if len(values) != 1 || values[0] != value {
			t.Fatalf("%s = %#v, want [%q]", key, values, value)
		}
	}
}

func TestWithClaudeModelEnvDefaultsAuxiliaryModelsToPrimaryModel(t *testing.T) {
	env := withClaudeModelEnv(nil, "deepseek-flash[1m]", "", "")
	want := map[string]string{
		"ANTHROPIC_MODEL":                "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "deepseek-flash[1m]",
		"CLAUDE_CODE_SUBAGENT_MODEL":     "deepseek-flash[1m]",
	}
	for _, entry := range env {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				key, value := entry[:i], entry[i+1:]
				if want[key] != value {
					t.Fatalf("%s = %q, want %q", key, value, want[key])
				}
				delete(want, key)
				break
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing defaulted model environment variables: %#v", want)
	}
}
