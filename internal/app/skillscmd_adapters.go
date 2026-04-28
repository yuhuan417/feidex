package app

import (
	appskillscmd "feidex/internal/app/skillscmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// newSkillsService creates a skillscmd.Service with callbacks wired to *App.
func newSkillsService(a *App) *appskillscmd.Service {
	s := appskillscmd.NewService()
	s.FeishuClient = func() appskillscmd.FeishuClient { return a.feishu }
	s.RequireCodexClient = func() (appskillscmd.CodexClient, error) {
		return requireCodexClient(a)
	}
	s.AppStateSession = func(sessionKey string) *state.Session {
		return a.State().Session(sessionKey)
	}
	s.DefaultWorkspaceID = func() string {
		return defaultWorkspaceID(a)
	}
	s.FindWorkspace = func(workspaceID string) *config.Workspace {
		return config.FindWorkspace(a.cfg, workspaceID)
	}
	s.FormatMenuBody = menuCardBody
	s.CommandLabel = commandLabel
	s.GetPendingSkillTracker = func() *appskillscmd.PendingSkillTracker {
		if a == nil {
			return nil
		}
		if a.trackers.pendingSkills == nil {
			a.trackers.pendingSkills = appskillscmd.NewPendingSkillTracker()
		}
		return a.trackers.pendingSkills
	}
	s.MakeSessionKey = func(msg *feishu.InboundMessage) string {
		return makeSessionKey(a, msg)
	}
	s.ReplyInThreadEnabled = func(chatType string) bool {
		return replyInThreadEnabled(a, chatType)
	}
	return s
}

// skillsService is the type alias for backward compatibility.
type skillsService = *appskillscmd.Service

// matchSkillsCommand is the alias for the exported command matcher.
var matchSkillsCommand = appskillscmd.MatchSkillsCommand
