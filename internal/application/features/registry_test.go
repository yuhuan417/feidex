package features

import (
	"strings"
	"testing"
)

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

func TestEveryDeclaredMenuNodeLeadsBackToMenu(t *testing.T) {
	nodes := MenuNodes()
	if _, ok := nodes["menu.root"]; !ok {
		t.Fatal("menu.root is missing")
	}
	for action := range nodes {
		seen := map[string]bool{}
		for current := action; current != "menu.root"; {
			if seen[current] {
				t.Fatalf("menu node %q has a parent cycle at %q", action, current)
			}
			seen[current] = true
			node, ok := nodes[current]
			if !ok || node.Parent == "" {
				t.Fatalf("menu node %q is disconnected at %q", action, current)
			}
			current = node.Parent
		}
	}
}

// TestMenuItemsCarryDirectCommandEntrypoint keeps the DEVELOPER.md contract
// that any capability reachable from a menu is also invocable from a slash
// command: every visible menu item must carry a declared command. Back items
// are navigation-only by definition, and items that open a pure navigation
// group page are exempt because the group's items carry their own commands.
func TestMenuItemsCarryDirectCommandEntrypoint(t *testing.T) {
	groups := map[string]bool{}
	for _, group := range MenuGroupSpecs() {
		groups[strings.TrimSpace(group.Action)] = true
	}
	for _, item := range MenuItemSpecs() {
		if item.Kind == MenuItemBack {
			continue
		}
		slash := strings.TrimSpace(item.Slash)
		if slash == "" {
			if groups[strings.TrimSpace(item.Action)] {
				continue
			}
			t.Fatalf("menu item %q (%s) has no slash entrypoint", item.Action, item.Kind)
		}
		// Reuse the runtime router so slash values like "/backend retry"
		// validate the same way menu visibility filters them per backend.
		routable := false
		for _, kind := range []string{"codex", "claude"} {
			if HandlesCommand(kind, slash) {
				routable = true
				break
			}
		}
		if !routable {
			t.Fatalf("menu item %q slash %q does not resolve to any backend command", item.Action, slash)
		}
	}
}
