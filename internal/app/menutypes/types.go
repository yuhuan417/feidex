package menutypes

import appfeatures "feidex/internal/app/features"

type MenuItemKind = appfeatures.MenuItemKind

const (
	MenuItemDirect  = appfeatures.MenuItemDirect
	MenuItemSubmenu = appfeatures.MenuItemSubmenu
	MenuItemBack    = appfeatures.MenuItemBack
)

type HelpCommandSpec = appfeatures.HelpCommandSpec
type MenuGroupSpec = appfeatures.MenuGroupSpec
type MenuItemSpec = appfeatures.MenuItemSpec
type MenuNode = appfeatures.MenuNode
