package app

import (
	appmenuutil "feidex/internal/adapter/feishu/menuutil"
	menutypes "feidex/internal/application/features"
	"feidex/internal/feishu"
)

func menuGroupSpec(action string) (menutypes.MenuGroupSpec, bool) {
	return appmenuutil.MenuGroupSpec(action)
}
func menuActionVisibleForBackend(action, backend string) bool {
	return menutypes.MenuActionVisible(action, backend)
}
func nearestVisibleMenuAction(action, backend string) string {
	return menutypes.NearestVisibleMenuAction(action, backend)
}
func renderRootMenuButtons(backend, sessionKey string) []feishu.Button {
	return appmenuutil.RenderRootMenuButtons(backend, sessionKey, menutypes.RootMenuGroups(backend))
}
func renderGroupMenuButtons(backend, groupAction, sessionKey string) []feishu.Button {
	return appmenuutil.RenderGroupMenuButtons(sessionKey, menutypes.MenuItemsForGroup(groupAction, backend))
}
