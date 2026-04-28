package app

import (
	appthreadmenu "feidex/internal/app/threadmenu"
	appthreadview "feidex/internal/app/threadview"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type threadService = appthreadmenu.Service

var newThreadService = appthreadmenu.NewService

var renderThreadSettingValue = appthreadview.RenderThreadSettingValue

var renderThreadButtonLabel = appthreadview.RenderThreadButtonLabel

var renderThreadListEntry = appthreadview.RenderThreadListEntry

var renderThreadListEntryBase = appthreadview.RenderThreadListEntryBase

var shortThreadID = appthreadview.ShortThreadID

var filterThreadsByWorkspaceCWD = appthreadview.FilterThreadsByWorkspaceCWD

var sameWorkspaceCWD = appthreadview.SameWorkspaceCWD

func currentThreadLabel(sess *state.Session) string {
	return appthreadmenu.SessionCurrentThreadLabel(sess)
}

func commandInterrupt(a *App, msg *feishu.InboundMessage) error {
	return appthreadmenu.NewService(a).CommandInterrupt(msg)
}

func commandAppend(a *App, msg *feishu.InboundMessage, text string) error {
	return appthreadmenu.NewService(a).CommandAppend(msg, text)
}

func showThreadSandboxMenu(a *App, msg *feishu.InboundMessage) error {
	return appthreadmenu.NewService(a).ShowThreadSandboxMenu(msg)
}

func renderThreadSandboxMenuCard(a *App, sessionKey string) (map[string]any, error) {
	return appthreadmenu.NewService(a).RenderThreadSandboxMenuCard(sessionKey)
}

func showThreadPolicyMenu(a *App, msg *feishu.InboundMessage) error {
	return appthreadmenu.NewService(a).ShowThreadPolicyMenu(msg)
}

func renderThreadPolicyMenuCard(a *App, sessionKey string) (map[string]any, error) {
	return appthreadmenu.NewService(a).RenderThreadPolicyMenuCard(sessionKey)
}

const threadCommandUsage = appthreadmenu.ThreadCommandUsage
