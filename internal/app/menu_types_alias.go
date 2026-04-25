package app

import "feidex/internal/app/menutypes"

type helpCommandSpec = menutypes.HelpCommandSpec
type commandMenuGroupSpec = menutypes.MenuGroupSpec
type commandMenuItemSpec = menutypes.MenuItemSpec

const menuItemDirect = menutypes.MenuItemDirect
const menuItemSubmenu = menutypes.MenuItemSubmenu
const menuItemBack = menutypes.MenuItemBack

var menuNodes = menutypes.MenuNodes
var commandMenuGroupSpecs = menutypes.MenuGroupSpecs
var commandMenuItemSpecs = menutypes.MenuItemSpecs
