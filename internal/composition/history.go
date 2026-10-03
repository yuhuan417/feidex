package composition

import (
	"context"
	"fmt"

	claudecatalog "feidex/internal/adapter/backend/claude/catalog"
	codexadapter "feidex/internal/adapter/backend/codex"
	historycards "feidex/internal/adapter/feishu/history"
	"feidex/internal/application/backendops"
	historyapp "feidex/internal/application/history"
	"feidex/internal/domain/backend"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
)

type HistoryDependencies struct {
	Frontend identity.FrontendID

	Repository    historyapp.Repository
	Backend       func() string
	CodexClient   func() codexadapter.RPCClient
	Context       func() context.Context
	Outbound      historycards.Outbound
	SessionKey    func(*feishu.InboundMessage) string
	ReplyInThread func(string) bool
}

func NewHistory(deps HistoryDependencies) historycards.Service {
	queries := historyapp.Service{
		Frontend:   deps.Frontend,
		Repository: deps.Repository, Backend: deps.Backend,
		Readers: map[string]historyapp.Reader{
			backend.BackendCodex:  currentCodexHistory{client: deps.CodexClient},
			backend.BackendClaude: claudecatalog.HistoryReader{},
		},
	}
	return historycards.NewService(historycards.Dependencies{
		Context: deps.Context, Outbound: deps.Outbound, Queries: queries,
		SessionKey: deps.SessionKey, ReplyInThread: deps.ReplyInThread,
	})
}

// Each query resolves the current client so transport recovery does not leave
// a long-lived query service pointing at a closed app-server connection.
type currentCodexHistory struct{ client func() codexadapter.RPCClient }

func (r currentCodexHistory) ReadConversationHistory(ctx context.Context, id string) (backendops.ThreadHistory, error) {
	var client codexadapter.RPCClient
	if r.client != nil {
		client = r.client()
	}
	if client == nil {
		return backendops.ThreadHistory{}, fmt.Errorf("codex client unavailable")
	}
	return (codexadapter.Gateway{Client: client}).ReadConversationHistory(ctx, id)
}
