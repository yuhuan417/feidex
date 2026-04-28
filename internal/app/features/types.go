package features

import (
	"strings"

	appruntime "feidex/internal/app/runtime"
)

// SpecKind identifies whether a registry entry is a user capability or a
// structural menu section.
type SpecKind string

const (
	SpecKindCapability SpecKind = "capability"
	SpecKindSection    SpecKind = "section"
)

// MenuItemKind identifies the type of a menu item.
type MenuItemKind string

const (
	MenuItemDirect  MenuItemKind = "direct"
	MenuItemSubmenu MenuItemKind = "submenu"
	MenuItemBack    MenuItemKind = "back"
)

// HelpCommandSpec describes a single help command entry.
type HelpCommandSpec struct {
	Command string
	Summary string
}

// CommandBackendSpec describes backend-specific command presentation.
type CommandBackendSpec struct {
	HideInHelp  bool
	HelpEntries []HelpCommandSpec
}

// CommandSpec describes one direct command entrypoint and its help metadata.
type CommandSpec struct {
	ID          string
	Names       []string
	HelpGroup   string
	HelpEntries []HelpCommandSpec
	Backends    map[string]CommandBackendSpec
}

// MenuGroupSpec describes a top-level menu group.
type MenuGroupSpec struct {
	Action      string
	Label       string
	Description string
	ShowInRoot  bool
}

// MenuItemSpec describes a leaf item within a menu group.
type MenuItemSpec struct {
	GroupAction         string
	Action              string
	Label               string
	Slash               string
	Kind                MenuItemKind
	IncludeParentAction bool
}

// MenuNode is a node in the menu navigation tree.
type MenuNode struct {
	Action string
	Label  string
	Parent string
}

// Spec is the single declaration source for a user capability or structural
// menu section.
type Spec struct {
	ID          string
	Kind        SpecKind
	Backends    []string
	Commands    []CommandSpec
	Nodes       []MenuNode
	MenuGroup   *MenuGroupSpec
	MenuItems   []MenuItemSpec
	ActionNames []string
}

// SupportsBackend reports whether the feature should be visible for the
// backend in user-facing menus/help.
func (s Spec) SupportsBackend(backend string) bool {
	backend = appruntime.NormalizeBackend(backend)
	if backend == "" || len(s.Backends) == 0 {
		return true
	}
	for _, candidate := range s.Backends {
		if appruntime.NormalizeBackend(candidate) == backend {
			return true
		}
	}
	return false
}

// BackendSpec returns the backend-specific command override for the backend.
func (s CommandSpec) BackendSpec(backend string) CommandBackendSpec {
	backend = appruntime.NormalizeBackend(backend)
	if backend == "" {
		return CommandBackendSpec{}
	}
	if s.Backends == nil {
		return CommandBackendSpec{}
	}
	if spec, ok := s.Backends[backend]; ok {
		return spec
	}
	return CommandBackendSpec{}
}

// HelpEntriesForBackend returns the backend-specific help entries.
func (s CommandSpec) HelpEntriesForBackend(backend string) []HelpCommandSpec {
	backendSpec := s.BackendSpec(backend)
	if backendSpec.HideInHelp {
		return nil
	}
	if backendSpec.HelpEntries != nil {
		return append([]HelpCommandSpec(nil), backendSpec.HelpEntries...)
	}
	return append([]HelpCommandSpec(nil), s.HelpEntries...)
}

// VisibleInHelpForBackend reports whether the command should appear in /help.
func (s CommandSpec) VisibleInHelpForBackend(backend string) bool {
	return !s.BackendSpec(backend).HideInHelp
}

// OwnsAction reports whether the feature owns the action or menu node.
func (s Spec) OwnsAction(action string) bool {
	action = strings.TrimSpace(action)
	if action == "" {
		return false
	}
	for _, name := range s.ActionNames {
		if strings.TrimSpace(name) == action {
			return true
		}
	}
	for _, node := range s.Nodes {
		if strings.TrimSpace(node.Action) == action {
			return true
		}
	}
	for _, item := range s.MenuItems {
		if strings.TrimSpace(item.Action) == action {
			return true
		}
	}
	if s.MenuGroup != nil && strings.TrimSpace(s.MenuGroup.Action) == action {
		return true
	}
	return false
}
