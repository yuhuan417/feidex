package feishuapp

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

func CompactionPorts(a *App) compaction.Dependencies {
	if a == nil {
		return compaction.Dependencies{}
	}
	st := a.State()
	return compaction.Dependencies{
		Context: a.Context, Repository: compactSessionStoreAdapter{Session: st.Session, Sessions: st.Sessions, Save: st.SaveSession},
		Gateway: compactGateway{client: func() (CodexClient, error) { return a.runtimeView().requireCodexClient() }},
		Notices: func(ctx context.Context, sess *conversation.Session, text string) {
			if a.feishu != nil && sess.ChatID != "" {
				_ = sendTextEffect(ctx, a, sess.ChatID, text)
			}
		},
	}
}

type compactGateway struct{ client func() (CodexClient, error) }

func (g compactGateway) StartCompaction(ctx context.Context, threadID string) error {
	client, err := g.client()
	if err != nil {
		return err
	}
	return (codexadapter.Gateway{Client: client}).StartCompaction(ctx, threadID)
}
func commandCompact(a *App, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /compact")
	}
	if a == nil {
		return nil
	}
	return a.bindings.BackendActions.HandleCompactCommand(msg, a.bindings.Compaction)
}
func runMenuCompactAction(a *App, action *feishu.CardAction, key string) error {
	if a == nil {
		return nil
	}
	return a.bindings.BackendActions.RunMenuCompactAction(action, key, a.bindings.Compaction)
}
