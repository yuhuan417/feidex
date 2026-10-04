package feishuapp

import (
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/state"
	"feidex/internal/textutil"
	"strings"
)

type turnStopStateProvider interface {
	Session(string) *conversation.Session
	PendingRequests() []*state.PendingRequest
}

func turnStopAttentionUserID(store turnStopStateProvider, sub *domainsubmission.Submission, turnID string) string {
	if !shouldMentionOnTurnStop(store, sub, turnID) {
		return ""
	}
	return strings.TrimSpace(sub.UserID)
}

func shouldMentionOnTurnStop(store turnStopStateProvider, sub *domainsubmission.Submission, turnID string) bool {
	if store == nil || sub == nil || strings.TrimSpace(sub.UserID) == "" {
		return false
	}
	turnID = textutil.FirstNonEmpty(strings.TrimSpace(turnID), strings.TrimSpace(sub.TurnID))
	sess := store.Session(sub.SessionKey)
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
	for _, req := range store.PendingRequests() {
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
