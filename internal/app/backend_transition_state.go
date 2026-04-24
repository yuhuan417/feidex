package app

import "strings"

func (s runtimeStateService) beginBackendSwitchState(target string) {
	if s.app == nil {
		return
	}
	s.app.backendStateMu.Lock()
	defer s.app.backendStateMu.Unlock()
	s.app.backendSwitching = true
	s.app.backendSwitchTarget = normalizeRuntimeBackend(target)
}

func (s runtimeStateService) finishBackendSwitchState() {
	if s.app == nil {
		return
	}
	s.app.backendStateMu.Lock()
	defer s.app.backendStateMu.Unlock()
	s.app.backendSwitching = false
	s.app.backendSwitchTarget = ""
}

func (s runtimeStateService) backendSwitchState() (bool, string) {
	if s.app == nil {
		return false, ""
	}
	s.app.backendStateMu.Lock()
	defer s.app.backendStateMu.Unlock()
	return s.app.backendSwitching, s.app.backendSwitchTarget
}

func (s runtimeStateService) backendSwitchBlockedReasonForTraffic() string {
	switching, target := s.backendSwitchState()
	if !switching {
		return ""
	}
	if display := backendDisplayName(strings.TrimSpace(target)); display != "" {
		return "当前正在切换到 " + display + " backend，请稍后再试"
	}
	return "当前正在切换 backend，请稍后再试"
}

func (s runtimeStateService) backendSwitchBlocksCardAction(actionName string) string {
	if strings.TrimSpace(actionName) == "menu.backend.switch" {
		return ""
	}
	return s.backendSwitchBlockedReasonForTraffic()
}
