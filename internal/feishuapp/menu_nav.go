package feishuapp

import (
	appmenuutil "feidex/internal/adapter/feishu/menuutil"
)

func menuCardBody(action, body string) string {
	return appmenuutil.MenuCardBody(action, body)
}

func menuCardBodyForBackend(backend, action, body string) string {
	return appmenuutil.MenuCardBodyForBackend(backend, action, body)
}

func commandLabel(label, slash string) string {
	return appmenuutil.CommandLabel(label, slash)
}

func submenuCommandLabel(label, slash string) string {
	return appmenuutil.SubmenuCommandLabel(label, slash)
}
