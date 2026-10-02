package app

import (
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/textutil"
	"strings"
)

func turnStopAttentionUserID(a *App, sub *domainsubmission.Submission, turnID string) string {
	if !shouldMentionOnTurnStop(a, sub, turnID) {
		return ""
	}
	return strings.TrimSpace(sub.UserID)
}

func shouldMentionOnTurnStop(a *App, sub *domainsubmission.Submission, turnID string) bool {
	if a == nil || sub == nil || strings.TrimSpace(sub.UserID) == "" {
		return false
	}
	turnID = textutil.FirstNonEmpty(strings.TrimSpace(turnID), strings.TrimSpace(sub.TurnID))
	sess := a.State().Session(sub.SessionKey)
	if sess == nil {
		return true
	}
	cp := *sess
	cp.Queue = append([]string(nil), sess.Queue...)
	cp.StagedImages = append([]conversation.SessionStagedImage(nil), sess.StagedImages...)
	cp.ActiveOperations = append([]conversation.SessionActiveOperation(nil), sess.ActiveOperations...)
	conversation.RemoveActiveOperation(&cp, sub.ID, turnID)
	if sessionHasActiveWork(&cp) {
		return false
	}
	if len(cp.Queue) > 0 || len(cp.StagedImages) > 0 {
		return false
	}
	for _, req := range a.State().PendingRequests() {
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
