package app

import (
	appmenuutil "feidex/internal/app/menuutil"
)

func menuBreadcrumbLabels(action string) []string {
	return appmenuutil.MenuBreadcrumbLabels(action)
}

func menuBreadcrumbLabelsForBackend(action, backend string) []string {
	return appmenuutil.MenuBreadcrumbLabelsForBackend(action, backend)
}

func menuCardBody(action, body string) string {
	return appmenuutil.MenuCardBody(action, body)
}

func menuCardBodyForBackend(backend, action, body string) string {
	return appmenuutil.MenuCardBodyForBackend(backend, action, body)
}

func menuNodeLabelForBackend(action, label, backend string) string {
	return appmenuutil.MenuNodeLabelForBackend(action, label, backend)
}

func submenuLabel(label string) string {
	return appmenuutil.SubmenuLabel(label)
}

func commandLabel(label, slash string) string {
	return appmenuutil.CommandLabel(label, slash)
}

func submenuCommandLabel(label, slash string) string {
	return appmenuutil.SubmenuCommandLabel(label, slash)
}
