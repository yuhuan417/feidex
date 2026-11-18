package feishuapp

import (
	"context"
	backendadapter "feidex/internal/adapter/backend"
	claudeadapter "feidex/internal/adapter/backend/claude"
	codexadapter "feidex/internal/adapter/backend/codex"
	"feidex/internal/adapter/feishu/threadview"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/conversation"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/compositionkit"
	"feidex/internal/config"
	domainbackend "feidex/internal/domain/backend"
	domain "feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	frontendruntime "feidex/internal/runtime"
	appcodexruntime "feidex/internal/runtime/codex"
	"strings"
	"sync"
)

type ConversationPortInputs struct {
	Config                    *config.Config
	ConfigMu                  *sync.RWMutex
	FrontendID                string
	FrontendConfigIndex       int
	Context                   func() context.Context
	Repository                *appstate.Store
	RuntimeOwner              *frontendruntime.FrontendOwner
	LiveThreads               conversation.LiveThreads
	ModelSettings             conversation.ModelSettings
	ThreadBinding             conversation.ThreadBindingDependencies
	ConversationConfiguration conversation.Configuration
	ContinueClaude            func(string, string) error
}

func ConversationPorts(inputs ConversationPortInputs) conversation.Dependencies {
	view := frontendConfigView{cfg: inputs.Config, mu: inputs.ConfigMu, frontendID: inputs.FrontendID, frontendConfigIndex: inputs.FrontendConfigIndex}
	runtime := runtimeView{owner: inputs.RuntimeOwner}
	backend := func() string {
		current := view
		if inputs.RuntimeOwner != nil {
			current.backend = inputs.RuntimeOwner.Backend()
		}
		return current.configuredBackend()
	}
	deps := conversation.Dependencies{
		Context: inputs.Context, Backend: backend,
		Repository: compositionkit.ConversationRepository{
			Repository: inputs.Repository, Runner: newEffectRunner(inputs.RuntimeOwner),
			Frontend: identity.FrontendID(inputs.FrontendID), Context: inputs.Context,
		},
		Live: inputs.LiveThreads, ModelSettings: inputs.ModelSettings, Operations: inputs.Repository,
		ThreadBinding: inputs.ThreadBinding,
	}
	deps.Gateway = backendadapter.ConversationGateway{Selected: deps.Backend, Gateways: map[string]conversation.Gateway{
		domainbackend.BackendClaude: claudeadapter.ConversationGateway{Client: func() claudeadapter.ConversationClient { return runtime.currentClaudeCore() }, Continue: inputs.ContinueClaude},
		domainbackend.BackendCodex: codexadapter.ConversationGateway{
			Client:        func() (codexadapter.ConversationClient, error) { return runtime.requireCodexClient() },
			Configuration: inputs.ConversationConfiguration,
		},
	}}
	return deps
}

type threadCardInputs struct {
	Repository         *appstate.Store
	Config             *config.Config
	Backend            func() string
	Conversations      *conversation.Service
	WorkspaceSelection workspaceapp.SelectionService
}

func renderThreadsCard(inputs threadCardInputs, key string, all bool) (map[string]any, error) {
	sess := inputs.Repository.Session(key)
	workspaceID := ""
	// Group workspace selection is persisted on the current Bot binding. It
	// intentionally does not rewrite an existing session until the next user
	// input, so resolve it before falling back to the session snapshot here.
	if selected := inputs.WorkspaceSelection.BindingWorkspace(key, sess); selected != "" {
		workspaceID = selected
	}
	if workspaceID == "" && sess != nil {
		workspaceID = strings.TrimSpace(sess.WorkspaceID)
	}
	ws := &inputs.Config.Workspaces[0]
	if selected := config.FindWorkspace(inputs.Config, workspaceID); selected != nil {
		ws = selected
	}
	items, err := inputs.Conversations.ListWorkspaceThreads(key, ws, all)
	if err != nil {
		return nil, err
	}
	if inputs.Backend() == domainbackend.BackendClaude {
		return threadview.RenderClaudeThreadsCard(key, sess, ws, inputs.Backend(), inputs.Config.Claude, items, all)
	}
	return threadview.RenderCodexThreadsCard(key, sess, *ws, inputs.Backend(), items, all)
}
func forkReplyMessage(backend, id string) string {
	if backend == domainbackend.BackendClaude {
		if strings.TrimSpace(id) == "" {
			return "prepared to fork current session. new Claude branch session will be created and switched on next message."
		}
		return "forked current session and switched to new branch session."
	}
	return "forked current thread and switched to new branch thread."
}
func interruptConversation(conversations *conversation.Service, deps BackendRuntimeDeps, ctx context.Context, key string, sess *domain.Session) error {
	err := conversations.InterruptActiveTurn(ctx, key, sess)
	deps = deps.currentBackend()
	if err != nil && sess != nil && deps.view.configuredBackend() == domainbackend.BackendCodex && deps.turnReconciliation != nil {
		updated := reconcileCompletedCodexTurn(*deps.turnReconciliation, sess.Key, sess)
		if updated == nil || updated.ActiveTurnID != sess.ActiveTurnID {
			return nil
		}
	}
	return err
}
func ConversationRecoveryPorts(
	cfg *config.Config,
	mu *sync.RWMutex,
	frontendConfigIndex int,
	repository conversation.StartupRepository,
	conversations *conversation.Service,
	owner *frontendruntime.FrontendOwner,
	codexRecovery appcodexruntime.RecoveryService,
	conversationConfiguration codexadapter.ConversationConfiguration,
) conversation.RecoveryDependencies {
	view := frontendConfigView{cfg: cfg, mu: mu, frontendConfigIndex: frontendConfigIndex}
	runtime := runtimeView{owner: owner}
	return conversation.RecoveryDependencies{Repository: repository, Conversations: conversations, Workspaces: planWorkspaces{view: view}, Capture: func() (conversation.RecoveryEndpoint, error) {
		currentView := view
		if owner != nil {
			currentView.backend = owner.Backend()
		}
		if currentView.configuredBackend() == domainbackend.BackendClaude {
			return conversation.RecoveryEndpoint{LazyResume: true}, nil
		}
		client, err := runtime.requireCodexClient()
		if err != nil {
			return conversation.RecoveryEndpoint{}, err
		}
		gateway := codexadapter.ConversationGateway{
			Client:        func() (codexadapter.ConversationClient, error) { return client, nil },
			Configuration: conversationConfiguration,
		}
		return conversation.RecoveryEndpoint{Gateway: gateway, Current: func() bool {
			return !codexRuntimeRecovering(codexRecovery) && runtime.currentCodexClient() == client
		}}, nil
	}}
}
