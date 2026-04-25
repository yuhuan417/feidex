package app

import (
	appthreadmenu "feidex/internal/app/threadmenu"
	appthreadview "feidex/internal/app/threadview"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// Type and constructor aliases — threadmenu sub-package
// ---------------------------------------------------------------------------

type threadService = appthreadmenu.Service

var newThreadService = appthreadmenu.NewService

// ---------------------------------------------------------------------------
// Thread view aliases — re-exported for callers in app/
// ---------------------------------------------------------------------------

var renderThreadSettingValue = appthreadview.RenderThreadSettingValue

var renderThreadButtonLabel = appthreadview.RenderThreadButtonLabel

var renderThreadListEntry = appthreadview.RenderThreadListEntry

var renderThreadListEntryBase = appthreadview.RenderThreadListEntryBase

var shortThreadID = appthreadview.ShortThreadID

var filterThreadsByWorkspaceCWD = appthreadview.FilterThreadsByWorkspaceCWD

var sameWorkspaceCWD = appthreadview.SameWorkspaceCWD

// currentThreadLabel returns a display label for the active thread.
func currentThreadLabel(sess *state.Session) string {
	return appthreadmenu.SessionCurrentThreadLabel(sess)
}

// ---------------------------------------------------------------------------
// Thin wrappers — free functions that delegate to the sub-package service
// ---------------------------------------------------------------------------

func startFreshThread(a *App, sessionKey, userID, chatID, chatType string) (int, *workspaceThreadBinding, error) {
	discarded, binding, err := newThreadService(a).StartFreshThread(sessionKey, userID, chatID, chatType)
	if err != nil {
		return 0, nil, err
	}
	return discarded, binding, nil
}

func commandInterrupt(a *App, msg *feishu.InboundMessage) error {
	return newThreadService(a).CommandInterrupt(msg)
}

func commandAppend(a *App, msg *feishu.InboundMessage, text string) error {
	return newThreadService(a).CommandAppend(msg, text)
}

func showThreadSandboxMenu(a *App, msg *feishu.InboundMessage) error {
	return newThreadService(a).ShowThreadSandboxMenu(msg)
}

func renderThreadSandboxMenuCard(a *App, sessionKey string) (map[string]any, error) {
	return newThreadService(a).RenderThreadSandboxMenuCard(sessionKey)
}

func showThreadPolicyMenu(a *App, msg *feishu.InboundMessage) error {
	return newThreadService(a).ShowThreadPolicyMenu(msg)
}

func renderThreadPolicyMenuCard(a *App, sessionKey string) (map[string]any, error) {
	return newThreadService(a).RenderThreadPolicyMenuCard(sessionKey)
}

// threadCommandUsage is kept for backward compatibility with tests.
const threadCommandUsage = appthreadmenu.ThreadCommandUsage
