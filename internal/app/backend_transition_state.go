package app

import "strings"

func (a *App) beginBackendSwitchState(target string) {
	if a == nil {
		return
	}
	a.backendStateMu.Lock()
	defer a.backendStateMu.Unlock()
	a.backendSwitching = true
	a.backendSwitchTarget = normalizeRuntimeBackend(target)
}

func (a *App) finishBackendSwitchState() {
	if a == nil {
		return
	}
	a.backendStateMu.Lock()
	defer a.backendStateMu.Unlock()
	a.backendSwitching = false
	a.backendSwitchTarget = ""
}

func (a *App) backendSwitchState() (bool, string) {
	if a == nil {
		return false, ""
	}
	a.backendStateMu.Lock()
	defer a.backendStateMu.Unlock()
	return a.backendSwitching, a.backendSwitchTarget
}

func (a *App) backendSwitchBlockedReasonForTraffic() string {
	switching, target := a.backendSwitchState()
	if !switching {
		return ""
	}
	if display := backendDisplayName(strings.TrimSpace(target)); display != "" {
		return "当前正在切换到 " + display + " backend，请稍后再试"
	}
	return "当前正在切换 backend，请稍后再试"
}

func (a *App) backendSwitchBlocksCardAction(actionName string) string {
	if strings.TrimSpace(actionName) == "menu.backend.switch" {
		return ""
	}
	return a.backendSwitchBlockedReasonForTraffic()
}
