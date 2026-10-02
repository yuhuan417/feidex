package features

import "testing"

func TestMenuCapabilitiesDeclareDirectCommandEntrypoint(t *testing.T) {
	for _, spec := range All() {
		if spec.Kind != SpecKindCapability {
			continue
		}
		if len(spec.ActionNames) == 0 && len(spec.MenuItems) == 0 && spec.MenuGroup == nil {
			continue
		}
		if len(spec.Commands) == 0 {
			t.Fatalf("feature %q exposes menu/action surface without direct command entrypoint", spec.ID)
		}
	}
}

func TestFeatureActionNamesAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, spec := range All() {
		for _, actionName := range spec.ActionNames {
			name := actionName.String()
			if previous, exists := seen[name]; exists {
				t.Fatalf("action %q belongs to both %q and %q", name, previous, spec.ID)
			}
			seen[name] = spec.ID
		}
	}
}
