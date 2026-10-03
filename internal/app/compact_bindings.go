package app

import (
	"context"
	codexadapter "feidex/internal/adapter/backend/codex"
	compaction "feidex/internal/application/compaction"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
	"fmt"
)

type compactSessionStoreAdapter struct {
	Session  func(string) *conversation.Session
	Sessions func() []*conversation.Session
	Save     func(*conversation.Session) error
}

func (s compactSessionStoreAdapter) GetSession(key string) *conversation.Session {
	return s.Session(key)
}
func (s compactSessionStoreAdapter) AllSessions() []*conversation.Session { return s.Sessions() }
func (s compactSessionStoreAdapter) SaveSession(sess *conversation.Session) error {
	return s.Save(sess)
}
func newCompactionService(a *App) compaction.Service {
	if a == nil {
		return compaction.Service{}
	}
	st := a.State()
	return compaction.Service{Deps: compaction.Dependencies{
		Context: a.Context, Repository: compactSessionStoreAdapter{Session: st.Session, Sessions: st.Sessions, Save: st.SaveSession},
		Gateway: codexadapter.Gateway{Client: currentCodexClient(a)},
		Notices: func(ctx context.Context, sess *conversation.Session, text string) {
			if a.feishu != nil && sess.ChatID != "" {
				_ = sendTextEffect(ctx, a, sess.ChatID, text)
			}
		},
	}}
}
func commandCompact(a *App, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /compact")
	}
	if a == nil {
		return nil
	}
	return newBackendActionService(a).HandleCompactCommand(msg, newCompactionService(a))
}
func runMenuCompactAction(a *App, action *feishu.CardAction, key string) error {
	if a == nil {
		return nil
	}
	return newBackendActionService(a).RunMenuCompactAction(action, key, newCompactionService(a))
}
