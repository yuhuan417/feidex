package feishuapp

import (
	"context"
	codexadapter "feidex/internal/adapter/backend/codex"
	"feidex/internal/adapter/feishu/backend"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application"
	compaction "feidex/internal/application/compaction"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
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

func CompactionPorts(contextFn func() context.Context, st *appstate.Store, owner *frontendruntime.FrontendOwner, frontendID string, noticesEnabled bool) compaction.Dependencies {
	runtime := runtimeView{owner: owner}
	return compaction.Dependencies{
		Context: contextFn, Repository: compactSessionStoreAdapter{Session: st.Session, Sessions: st.Sessions, Save: st.SaveSession},
		Gateway: compactGateway{client: runtime.requireCodexClient},
		Notices: func(ctx context.Context, sess *conversation.Session, text string) {
			if noticesEnabled && sess.ChatID != "" {
				_ = newEffectRunner(owner).Run(ctx, []application.Effect{application.SendMessage{
					Frontend: identity.FrontendID(frontendID),
					Chat:     identity.ChatRef{ID: sess.ChatID},
					Text:     text,
				}})
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
func commandCompact(backendActions backend.ActionService, compactionService *compaction.Service, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /compact")
	}
	if compactionService == nil {
		return nil
	}
	return backendActions.HandleCompactCommand(msg, compactionService)
}
