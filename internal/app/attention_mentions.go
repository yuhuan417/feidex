package app

import (
	"strings"

	"feidex/internal/state"
)

func attentionMentionMarkdown(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	return "<at id=" + userID + "></at>"
}

func prependAttentionMentionMarkdown(body, userID string) string {
	body = strings.TrimSpace(body)
	mention := attentionMentionMarkdown(userID)
	if mention == "" {
		return body
	}
	if body == "" {
		return mention
	}
	if strings.HasPrefix(body, mention) {
		return body
	}
	return mention + "\n\n" + body
}

func (a *App) turnStopAttentionUserID(sub *state.Submission, turnID string) string {
	if !a.shouldMentionOnTurnStop(sub, turnID) {
		return ""
	}
	return strings.TrimSpace(sub.UserID)
}

func (a *App) shouldMentionOnTurnStop(sub *state.Submission, turnID string) bool {
	if a == nil || sub == nil || strings.TrimSpace(sub.UserID) == "" {
		return false
	}
	turnID = firstNonEmpty(strings.TrimSpace(turnID), strings.TrimSpace(sub.TurnID))
	sess := appState(a).session(sub.SessionKey)
	if sess == nil {
		return true
	}
	cp := *sess
	cp.Queue = append([]string(nil), sess.Queue...)
	cp.StagedImages = append([]state.SessionStagedImage(nil), sess.StagedImages...)
	cp.ActiveOperations = append([]state.SessionActiveOperation(nil), sess.ActiveOperations...)
	sessionRemoveActiveOperation(&cp, sub.ID, turnID)
	if sessionHasActiveWork(&cp) {
		return false
	}
	if len(cp.Queue) > 0 || len(cp.StagedImages) > 0 {
		return false
	}
	for _, req := range appState(a).pendingRequests() {
		if req == nil || !isPendingRequestOpen(req) {
			continue
		}
		if strings.TrimSpace(req.SessionKey) != strings.TrimSpace(sub.SessionKey) {
			continue
		}
		return false
	}
	return true
}
