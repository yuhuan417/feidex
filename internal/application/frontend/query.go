package frontend

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
)

type RuntimeFacts struct {
	SwitchBlockedReason                 string
	MessageTraffic                      int
	CodexMaintenance, ClaudeMaintenance bool
}
type ActivityRepository interface {
	Sessions() []*conversation.Session
	PendingRequests() []*interaction.PendingRequest
}
type Query struct {
	Repository ActivityRepository
	Facts      func() RuntimeFacts
	Retrying   func(string) bool
}

func (q Query) Activity(includeSessions bool) Activity {
	facts := q.Facts()
	activity := Activity{SwitchBlockedReason: facts.SwitchBlockedReason, MessageTraffic: facts.MessageTraffic}
	if facts.CodexMaintenance {
		activity.MaintenanceBlockedReason = "当前正在执行 Codex 维护，请稍后再切换 backend"
	} else if facts.ClaudeMaintenance {
		activity.MaintenanceBlockedReason = "当前正在执行 Claude 维护，请稍后再切换 backend"
	}
	if !includeSessions {
		return activity
	}
	for _, sess := range q.Repository.Sessions() {
		if sess != nil {
			activity.Sessions = append(activity.Sessions, SessionActivity{Session: sess, Retrying: q.Retrying(sess.Key)})
		}
	}
	for _, req := range q.Repository.PendingRequests() {
		if req != nil {
			activity.Requests = append(activity.Requests, interaction.Request{Status: req.Status})
		}
	}
	return activity
}
