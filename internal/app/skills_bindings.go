package app

import (
	"context"
	appskillscmd "feidex/internal/app/skillscmd"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
)

type skillsOutbound struct{ app *App }

func (o skillsOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return replyCardWithIDEffect(ctx, o.app, messageID, card, inThread)
}

func (o skillsOutbound) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	return replyTextByAnchorEffect(ctx, o.app, messageID, text, inThread)
}

// newSkillsService creates a skillscmd.Service with callbacks wired to *App.
func newSkillsService(a *App) *appskillscmd.Service {

	s := appskillscmd.NewService()
	s.Outbound = func() appskillscmd.Outbound { return skillsOutbound{app: a} }
	s.RequireCodexClient = func() (appskillscmd.CodexClient, error) {
		return requireCodexGateway(a)
	}
	s.AppStateSession = func(sessionKey string) *conversation.Session {
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
		trackers := a.Trackers()
		if trackers.pendingSkills == nil {
			trackers.pendingSkills = appskillscmd.NewPendingSkillTracker()
		}
		return trackers.pendingSkills
	}
	s.MakeSessionKey = func(msg *feishu.InboundMessage) string {
		return makeSessionKey(a, msg)
	}
	s.ReplyInThreadEnabled = func(chatType string) bool {
		return replyInThreadEnabled(a, chatType)
	}
	return s
}

// appskillscmd.MatchSkillsCommand is the alias for the exported command matcher.
