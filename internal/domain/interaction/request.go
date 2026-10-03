package interaction

import "strings"

// Request is the lifecycle state of a human interaction. Transport envelopes,
// card IDs, answer drafts and storage payloads belong to adapters.
type Request struct {
	ID         string
	Backend    string
	Kind       string
	Status     string
	SessionKey string
	ThreadID   string
	TurnID     string
}

// ReplyAccepted records a successful reply. Backend-resolved requests remain
// open until a separate authoritative resolution event arrives.
func (r Request) ReplyAccepted() Request {
	if !IsPendingRequestOpen(r.Status) {
		return r
	}
	r.Status = "resolved"
	if IsServerResolvedPendingKind(r.Kind) {
		r.Status = "replied"
	}
	return r
}

func (r Request) Resolved() (Request, bool) {
	if strings.TrimSpace(r.Status) == "resolved" || strings.TrimSpace(r.Status) == "expired" {
		return r, false
	}
	r.Status = "resolved"
	return r, true
}

// BlocksResume retains the existing matching rule: a same-turn or same-thread
// open server request blocks submission resumption.
func (r Request) BlocksResume(threadID, turnID, excludeID string) bool {
	if !IsServerResolvedPendingKind(r.Kind) || !IsPendingRequestOpen(r.Status) {
		return false
	}
	excludeID = strings.TrimSpace(excludeID)
	if excludeID != "" && strings.TrimSpace(r.ID) == excludeID {
		return false
	}
	threadID, turnID = strings.TrimSpace(threadID), strings.TrimSpace(turnID)
	return (turnID != "" && strings.TrimSpace(r.TurnID) == turnID) ||
		(threadID != "" && strings.TrimSpace(r.ThreadID) == threadID)
}
