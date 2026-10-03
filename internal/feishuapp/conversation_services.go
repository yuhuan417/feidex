package feishuapp

import (
	"context"
	backendadapter "feidex/internal/adapter/backend"
	claudeadapter "feidex/internal/adapter/backend/claude"
	codexadapter "feidex/internal/adapter/backend/codex"
	"feidex/internal/adapter/feishu/threadview"
	"feidex/internal/application/conversation"
	"feidex/internal/application/submission"
	"feidex/internal/compositionkit"
	"feidex/internal/config"
	domainbackend "feidex/internal/domain/backend"
	domain "feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"strings"
)

func ConversationPorts(a *App) conversation.Dependencies {
	s := conversation.Dependencies{Context: a.Context, Backend: func() string { return configuredBackend(a) }, Repository: compositionkit.ConversationRepository{Repository: a.State(), Runner: newEffectRunner(a), Frontend: identity.FrontendID(a.FrontendID()), Context: a.Context}, Live: sqLiveThreadAdapter{app: a}}
	s.ModelSettings = a.bindings.ModelSnapshots
	s.Operations = a.State()
	s.ThreadBinding = conversation.ThreadBindingDependencies{
		Lookup:   submission.SubmissionLookupService{State: a.State(), Runtime: a.runtimeOwner.TurnBindings},
		Bindings: a.runtimeOwner.TurnBindings, Replies: a.bindings.Continuation,
	}
	s.Gateway = backendadapter.ConversationGateway{Selected: s.Backend, Gateways: map[string]conversation.Gateway{
		domainbackend.BackendClaude: claudeadapter.ConversationGateway{Client: func() claudeadapter.ConversationClient { return currentClaudeCore(a) }, Continue: a.bindings.Continuation.ContinueClaudeSessionWithText},
		domainbackend.BackendCodex: codexadapter.ConversationGateway{
			Client:        func() (codexadapter.ConversationClient, error) { return requireCodexClient(a) },
			Configuration: a.bindings.ConversationConfiguration,
		},
	}}
	return s
}
func renderThreadsCard(a *App, key string, all bool) (map[string]any, error) {
	sess := a.State().Session(key)
	ws := &a.cfg.Workspaces[0]
	if sess != nil {
		if selected := config.FindWorkspace(a.cfg, sess.WorkspaceID); selected != nil {
			ws = selected
		}
	}
	items, err := a.bindings.Conversations.ListWorkspaceThreads(key, ws, all)
	if err != nil {
		return nil, err
	}
	if configuredBackend(a) == domainbackend.BackendClaude {
		return threadview.RenderClaudeThreadsCard(key, sess, ws, configuredBackend(a), a.cfg.Claude, items, all)
	}
	return threadview.RenderCodexThreadsCard(key, sess, *ws, configuredBackend(a), items, all)
}
func forkReplyMessage(a *App, id string) string {
	if configuredBackend(a) == domainbackend.BackendClaude {
		if strings.TrimSpace(id) == "" {
			return "prepared to fork current session. new Claude branch session will be created and switched on next message."
		}
		return "forked current session and switched to new branch session."
	}
	return "forked current thread and switched to new branch thread."
}
func renderConversationUsage(a *App, sess *domain.Session) string {
	if configuredBackend(a) == domainbackend.BackendClaude {
		return a.bindings.Usage.RenderClaudeUsageBody(sess)
	}
	return a.bindings.Usage.RenderCodexUsageBody(sess)
}
func interruptConversation(a *App, ctx context.Context, key string, sess *domain.Session) error {
	err := a.bindings.Conversations.InterruptActiveTurn(ctx, key, sess)
	if err != nil && sess != nil && configuredBackend(a) == domainbackend.BackendCodex {
		updated := reconcileCompletedCodexTurn(a, sess.Key, sess)
		if updated == nil || updated.ActiveTurnID != sess.ActiveTurnID {
			return nil
		}
	}
	return err
}
func ConversationRecoveryPorts(a *App) conversation.RecoveryDependencies {
	return conversation.RecoveryDependencies{Repository: a.State(), Conversations: a.bindings.Conversations, Workspaces: planWorkspaces{app: a}, Capture: func() (conversation.RecoveryEndpoint, error) {
		if configuredBackend(a) == domainbackend.BackendClaude {
			return conversation.RecoveryEndpoint{LazyResume: true}, nil
		}
		client, err := requireCodexClient(a)
		if err != nil {
			return conversation.RecoveryEndpoint{}, err
		}
		gateway := codexadapter.ConversationGateway{
			Client:        func() (codexadapter.ConversationClient, error) { return client, nil },
			Configuration: a.bindings.ConversationConfiguration,
		}
		return conversation.RecoveryEndpoint{Gateway: gateway, Current: func() bool { return !codexRuntimeRecovering(a) && currentCodexClient(a) == client }}, nil
	}}
}
