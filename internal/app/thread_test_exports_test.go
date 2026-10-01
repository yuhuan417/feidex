package app

import (
	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/feishu"
)

func commandInterrupt(a *App, msg *feishu.InboundMessage) error {
	return appthreadmenu.NewService(a).CommandInterrupt(msg)
}

func commandAppend(a *App, msg *feishu.InboundMessage, text string) error {
	return appthreadmenu.NewService(a).CommandAppend(msg, text)
}

func showThreadSandboxMenu(a *App, msg *feishu.InboundMessage) error {
	return appthreadmenu.NewService(a).ShowThreadSandboxMenu(msg)
}

func showThreadPolicyMenu(a *App, msg *feishu.InboundMessage) error {
	return appthreadmenu.NewService(a).ShowThreadPolicyMenu(msg)
}
