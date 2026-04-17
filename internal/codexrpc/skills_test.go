package codexrpc

import (
	"encoding/json"
	"testing"
)

func TestSkillsListResultUnmarshal(t *testing.T) {
	raw := []byte(`{
		"data": [{
			"cwd": "/repo",
			"skills": [{
				"name": "openai-docs",
				"description": "Lookup official OpenAI docs",
				"shortDescription": "Docs lookup",
				"interface": {
					"displayName": "OpenAI Docs",
					"shortDescription": "Lookup docs",
					"defaultPrompt": "Help me with docs"
				},
				"path": "/skills/openai-docs/SKILL.md",
				"scope": "system",
				"enabled": true
			}],
			"errors": [{
				"path": "/skills/bad/SKILL.md",
				"message": "invalid front matter"
			}]
		}]
	}`)

	var result SkillsListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("data len = %d, want 1", len(result.Data))
	}
	entry := result.Data[0]
	if entry.Cwd != "/repo" {
		t.Fatalf("cwd = %q, want /repo", entry.Cwd)
	}
	if len(entry.Skills) != 1 || entry.Skills[0].Name != "openai-docs" {
		t.Fatalf("skills = %+v, want openai-docs", entry.Skills)
	}
	if entry.Skills[0].Interface == nil || entry.Skills[0].Interface.DisplayName != "OpenAI Docs" {
		t.Fatalf("skill interface = %+v, want display name", entry.Skills[0].Interface)
	}
	if len(entry.Errors) != 1 || entry.Errors[0].Path != "/skills/bad/SKILL.md" {
		t.Fatalf("errors = %+v, want one error entry", entry.Errors)
	}
}
