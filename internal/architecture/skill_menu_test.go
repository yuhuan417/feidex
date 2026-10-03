package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillAdapterUsesApplicationAndOwnsNoBusinessState(t *testing.T) {
	violations, err := importsUnder(repositoryRoot(t), "internal/adapter/feishu/skills", []string{
		modulePath + "/internal/app", modulePath + "/internal/config", modulePath + "/internal/state",
		modulePath + "/internal/runtime", modulePath + "/internal/codexrpc", modulePath + "/internal/adapter/backend",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("skills adapter must consume application views and use cases: %v", violations)
	}
}

func TestCommandBindingsDoNotOwnRecognitionOrBackendPolicy(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{"internal/feishuapp/feature_registry_bindings.go", "internal/feishuapp/command_registry.go"} {
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"Match func", "IsLocal", "Backends map", "backendPolicy", "groupScopedHelpEntries"} {
			if strings.Contains(string(data), forbidden) {
				t.Errorf("%s reintroduced command policy %q", relative, forbidden)
			}
		}
	}
}
