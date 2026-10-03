package backend

import (
	"strings"
	"sync"

	domainbackend "feidex/internal/domain/backend"
)

// RuntimeStateService is one frontend's synchronized switch state.
type RuntimeStateService struct {
	mu        sync.Mutex
	operation sync.Mutex
	switching bool
	target    string
}

func (s *RuntimeStateService) LockSwitch()   { s.operation.Lock() }
func (s *RuntimeStateService) UnlockSwitch() { s.operation.Unlock() }
func (s *RuntimeStateService) BeginBackendSwitchState(target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.switching = true
	s.target = domainbackend.NormalizeBackend(target)
}
func (s *RuntimeStateService) FinishBackendSwitchState() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.switching = false
	s.target = ""
}
func (s *RuntimeStateService) BackendSwitchState() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.switching, s.target
}

// BackendSwitchBlockedReasonForTraffic returns a reason string if a backend
// switch is in progress, blocking traffic.
func (s *RuntimeStateService) BackendSwitchBlockedReasonForTraffic() string {
	switching, target := s.BackendSwitchState()
	if !switching {
		return ""
	}
	if display := BackendDisplayName(strings.TrimSpace(target)); display != "" {
		return "当前正在切换到 " + display + " backend，请稍后再试"
	}
	return "当前正在切换 backend，请稍后再试"
}

// BackendSwitchBlocksCardAction returns a reason string if a backend switch
// blocks the given card action.
func (s *RuntimeStateService) BackendSwitchBlocksCardAction(actionName string) string {
	if strings.TrimSpace(actionName) == "menu.backend.switch" {
		return ""
	}
	return s.BackendSwitchBlockedReasonForTraffic()
}
