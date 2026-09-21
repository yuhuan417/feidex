package app

import (
	"fmt"
	"strings"

	"feidex/internal/state"
)

// ensureSessionModelConfigIdle enforces the same idle-only rule for scoped
// model changes that the global configuration uses for the whole frontend.
func ensureSessionModelConfigIdle(a *App, sessionKey string) error {
	if a == nil || a.State() == nil {
		return nil
	}
	sessionKey = normalizeSessionKey(a, strings.TrimSpace(sessionKey))
	if sessionKey == "" {
		return nil
	}
	sess := a.State().Session(sessionKey)
	if sess == nil {
		return nil
	}
	if sessionHasActiveWork(sess) || len(sess.Queue) > 0 || len(sess.StagedImages) > 0 || state.NormalizeSessionStatus(sess.Status) != state.SessionStatusIdle {
		return fmt.Errorf("模型配置只能在当前 session 空闲时切换")
	}
	for _, req := range a.State().PendingRequests() {
		if req != nil && isPendingRequestOpen(req) && strings.TrimSpace(req.SessionKey) == sessionKey {
			return fmt.Errorf("模型配置只能在当前 session 空闲时切换")
		}
	}
	return nil
}
