package menutypes

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
	Label  string
	Parent string
}
