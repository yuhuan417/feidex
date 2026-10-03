package conversation

import "strings"

func NormalizeCollaborationMode(mode *SessionCollaborationMode) *SessionCollaborationMode {
	if mode == nil {
		return nil
	}
	cp := *mode
	cp.Mode, cp.Model, cp.ReasoningEffort = strings.TrimSpace(cp.Mode), strings.TrimSpace(cp.Model), strings.TrimSpace(cp.ReasoningEffort)
	if cp.Mode == "" || cp.Model == "" {
		return nil
	}
	return &cp
}
