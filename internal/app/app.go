package app

import (
	"feidex/internal/adapter/feishu/finalcardpatch"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/application"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/identity"
	domainsubmission "feidex/internal/domain/submission"

	"context"
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	workspacecards "feidex/internal/adapter/feishu/workspace"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/backendops"
	"feidex/internal/composition"
	"feidex/internal/domain/conversation"
	appcodexruntime "feidex/internal/runtime/codex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/goalcmd"
	appautoretry "feidex/internal/runtime/autoretry"

	"feidex/internal/adapter/feishu/serverrequest"

	"feidex/internal/adapter/feishu/turnitem"
	skillruntime "feidex/internal/runtime/skill"
	"feidex/internal/runtime/turnbinding"

	appthreadmenu "feidex/internal/adapter/feishu/threadmenu"
	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"

	frontendruntime "feidex/internal/runtime"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type App struct {
	cfg                    *config.Config
	cfgPath                string
	store                  *state.Store
	frontendID             string
	frontendConfigIndex    int
	configMu               sync.RWMutex
	sharedConfigMu         *sync.RWMutex
	backend                string
	backendDriver          backend.Driver
	feishu                 FeishuClient
	started                time.Time
	frontendRuntime        frontendruntime.FrontendRuntime
	stateMu                sync.Mutex
	stateView              *appstate.Store
	composition            *appComposition
	deduper                *frontendruntime.InboundDeduper
	asyncRunner            func(func())
	waitAsync              func()
	frontendRecoveryMu     sync.Mutex
	frontendTrafficMu      sync.Mutex
	frontendMessageTraffic int
	sessionActorsMu        sync.Mutex
	sessionActors          *frontendruntime.SessionActors
}

// appComposition owns lazily constructed application/backend services. Keeping
// these bindings together prevents the frontend aggregate from becoming a
// second service registry while preserving one cache per frontend runtime.
type appComposition struct {
	mu          sync.Mutex
	workspaceMu sync.Mutex
	clientsMu   sync.RWMutex
	// feishuTransport is used only by the effect runner; services get the proxy.
	feishuTransport FeishuClient
	// Runtime-owned state lives here so App remains the frontend entrypoint
	// rather than a registry of mutable service state.
	codex            CodexClient
	claude           ClaudeCore
	trackers         *appTrackers
	liveThreads      *frontendruntime.LiveThreads
	autoRetries      *appautoretry.Tracker
	codexRecovery    *appcodexruntime.RecoveryState
	threadMenu       *appthreadmenu.Service
	backendConfig    *backendConfigurationService
	backendSelection *backendSelectionService
	backendActions   *backend.ActionService
	workspaceConfig  *appworkspacecmd.ConfigService
	workspaceManage  *appworkspacecmd.ManagementService
	workspaceRender  *workspacecards.Presentation
	serverRequestSvc *serverrequest.Service
	dispatcher       *application.Dispatcher
	effectRunner     *frontendruntime.EffectRunner
	mcp              *feidexMCPService
	switchState      backend.RuntimeStateService
}

func (a *App) configMutex() *sync.RWMutex {
	if a == nil {
		return nil
	}
	if a.sharedConfigMu != nil {
		return a.sharedConfigMu
	}
	return &a.configMu
}

// appTrackers bundles per-service runtime trackers that are lazily initialized
// on first access. Each tracker is consumed by exactly one service type.
type appTrackers struct {
	turnStreams         *turnStreamTracker
	turnItems           *turnitem.Tracker
	turnBindings        *turnbinding.Tracker
	submissionStarts    frontendruntime.SubmissionStarts
	workspaceCloneOps   *appworkspacecmd.CloneTracker
	finalCardPatches    *finalcardpatch.Tracker
	pendingSkills       *skillruntime.Tracker
	groupAnnouncements  *groupAnnouncementTracker
	maintenanceTrackers backend.TrackerMap
	goals               *goalcmd.Tracker
}

func NewFrontend(scope composition.FrontendScope) (*App, error) {
	cfg, cfgPath, store, frontend := scope.Config, scope.ConfigPath, scope.Store, scope.Frontend
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	if store == nil {
		return nil, fmt.Errorf("nil store")
	}
	backend := normalizeRuntimeBackend(frontend.Backend)
	feishuTransport := appfeishuwrap.WrapFeishuClient(newFeishuClient(frontend.Feishu))
	app := &App{
		cfg:                 cfg,
		sharedConfigMu:      scope.ConfigMutex,
		cfgPath:             cfgPath,
		store:               store,
		frontendID:          strings.TrimSpace(frontend.ID),
		frontendConfigIndex: frontend.ConfigIndex,
		backend:             backend,
		backendDriver:       backendDriverForKind(backend),
		composition:         &appComposition{feishuTransport: feishuTransport},
		feishu:              feishuTransport,
		started:             time.Now(),
		deduper:             frontendruntime.NewInboundDeduper(),
		sessionActors:       frontendruntime.NewSessionActors(),
	}
	app.composition.liveThreads = frontendruntime.NewLiveThreads()
	app.composition.autoRetries = appautoretry.NewTracker()
	app.composition.trackers = &appTrackers{
		turnStreams:        newTurnStreamTracker(),
		turnItems:          turnitem.NewTracker(),
		workspaceCloneOps:  newWorkspaceCloneTracker(),
		turnBindings:       turnbinding.NewTracker(store),
		finalCardPatches:   finalcardpatch.NewTracker(),
		pendingSkills:      skillruntime.NewTracker(),
		groupAnnouncements: newGroupAnnouncementTracker(),
	}
	effectRunner := newEffectRunner(app)
	app.composition.effectRunner = &effectRunner
	if notifying, ok := app.feishu.(*appfeishuwrap.NotifyingFeishuClient); ok {
		app.feishu = &appfeishuwrap.EffectClient{NotifyingFeishuClient: notifying, Frontend: identity.FrontendID(app.frontendID), Runner: effectRunner}
	}
	app.stateView = appstate.NewScoped(app.store, app.FrontendID(), configuredBackend(app), allowLegacyFrontendFallback(app))
	app.stateView.RevisionMutex = app.ConfigMu()
	// Workspace presentation is a composition concern. Build it once after the
	// scoped state repository exists; its query reads detached snapshots and
	// follows runtime backend changes through the injected capability.
	app.composition.workspaceRender = buildWorkspaceRenderService(app)
	dispatcher := newInputDispatcher(app)
	app.composition.dispatcher = &dispatcher
	if err := canonicalizeStoredSessionKeys(app); err != nil {
		return nil, err
	}
	if backend != "" {
		handle, err := buildBackendRuntimeHandle(app, backend)
		if err != nil {
			return nil, err
		}
		handle.install(app)
	}
	app.feishu.SetHandlers(app.HandleFeishuMessage, app.HandleCardAction, app.HandleFeishuRecall, app.HandleFeishuReaction)
	configureGroupMessagePolicy(app)
	configureGroupPrimaryEvents(app)
	app.feishu.ConfigureLocalFileLinks("", "")
	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return (frontendruntime.FrontendGroup{Frontends: []frontendruntime.ManagedFrontend{a}}).Start(ctx)
}

func (a *App) beginLifecycle(ctx context.Context) {
	a.frontendRuntime.Begin(ctx)
}

func (a *App) Stop(ctx context.Context) error {
	if a == nil {
		return nil
	}
	a.frontendRuntime.Cancel()
	if a.feishu != nil {
		a.feishu.Stop()
	}
	backendErr := currentBackendRuntimeHandle(a).close()
	mcpErr := stopMCPService(a, ctx)
	if err := a.frontendRuntime.Wait(ctx); err != nil {
		return err
	}
	if backendErr != nil {
		return backendErr
	}
	return mcpErr
}

// Context returns the application lifecycle context for background work.
// It is cancelled before runtime shutdown so external calls can stop promptly.
func (a *App) Context() context.Context {
	if a == nil {
		return context.Background()
	}
	return a.frontendRuntime.Context()
}

func runAsync(a *App, fn func()) {
	if fn == nil {
		return
	}
	if a == nil {
		go fn()
		return
	}
	a.frontendRuntime.Run(fn, a.asyncRunner)
}

func buildThreadStartParams(a *App, ws *config.Workspace, sess *conversation.Session, effectiveModel string) codexrpc.ThreadStartParams {
	if strings.TrimSpace(effectiveModel) == "" {
		effectiveModel = modelConfigSnapshot(a, sess, domainbackend.BackendCodex).Model
	}
	return codexrpc.ThreadStartParams{
		Cwd:                    ws.Cwd,
		ApprovalPolicy:         effectiveBindingApprovalPolicy(a, sess, ws),
		Sandbox:                effectiveBindingSandboxMode(a, sess, ws),
		ServiceName:            a.cfg.Codex.ServiceName,
		ExperimentalRawEvents:  false,
		PersistExtendedHistory: true,
		ServiceTier:            strings.TrimSpace(effectiveBindingServiceTier(a, sess)),
		Model:                  strings.TrimSpace(effectiveModel),
		Config:                 codexAuxiliaryConfig(a, sess),
	}
}

func (a *App) HandleFeishuMessage(msg *feishu.InboundMessage) {
	if msg != nil {
		_, _ = dispatchInput(a, application.MessageReceived{Frontend: identity.FrontendID(a.FrontendID()), Chat: identity.ChatRef{Type: identity.ChatType(msg.ChatType), ID: msg.ChatID}, Message: *msg})
	}
}

func (a *App) HandleFeishuRecall(recall *feishu.MessageRecall) {
	if recall != nil {
		_, _ = dispatchInput(a, application.MessageRecalled{Frontend: identity.FrontendID(a.FrontendID()), MessageID: recall.MessageID, ChatID: recall.ChatID})
	}
}

func (a *App) HandleFeishuReaction(reaction *feishu.MessageReaction) {
	if reaction != nil {
		_, _ = dispatchInput(a, application.MessageReacted{Frontend: identity.FrontendID(a.FrontendID()), MessageID: reaction.MessageID, ChatID: reaction.ChatID, UserID: reaction.UserID, EmojiType: reaction.EmojiType})
	}
}

func isStaleInboundMessage(started time.Time, msg *feishu.InboundMessage) bool {
	if msg == nil || msg.CreatedAt == 0 {
		return false
	}
	return msg.CreatedAt < started.Add(-30*time.Second).Unix()
}

func nonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func (a *App) HandleCardAction(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return dispatchCardAction(a, action)
}

func enqueueSubmission(a *App, msg *feishu.InboundMessage) error {
	return enqueueSubmissionWithSessionKey(a, msg, makeSessionKey(a, msg), false)
}

func enqueueSubmissionWithSessionKey(a *App, msg *feishu.InboundMessage, sessionKey string, bindOnlyCurrentRoot bool) error {
	if err := newSubmissionQueueServiceFromApp(a).EnqueueSubmission(msg, sessionKey, bindOnlyCurrentRoot); err != nil {
		return err
	}
	invalidateCodexPlanModeExitArtifactsForSession(a, sessionKey, "当前已有新的提交，旧的计划确认已失效。")
	return nil
}

func startNextSubmission(a *App, sessionKey string) error {
	return newSubmissionQueueServiceFromApp(a).StartNextSubmission(sessionKey)
}

func startSubmissionTurn(a *App, ctx context.Context, sessionKey, threadID string, sub *domainsubmission.Submission, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort, multiAgentMode string) (string, error) {
	if sub == nil {
		return "", fmt.Errorf("nil submission")
	}
	request := backendops.StartTurnRequest{ThreadID: threadID, Submission: sub, Cwd: cwd, ApprovalPolicy: approvalPolicy, SandboxMode: sandboxMode, ServiceTier: serviceTier, Model: model, Effort: reasoningEffort, MultiAgentMode: multiAgentMode}
	if snapshot := sub.ModelConfig; snapshot.Valid {
		if snapshot.CollaborationMode != "" {
			selectedModel, selectedEffort := snapshot.Model, snapshot.Effort
			if snapshot.CollaborationMode == "plan" {
				selectedModel, selectedEffort = snapshot.PlanModel, snapshot.PlanEffort
			}
			if selectedModel != "" {
				request.Collaboration = &conversation.SessionCollaborationMode{Mode: snapshot.CollaborationMode, Model: selectedModel, ReasoningEffort: selectedEffort}
			}
		}
	} else {
		request.Collaboration = planModeStateForTurnStart(a, sessionKey, threadID)
	}
	slog.Debug("turn start request",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"approval_policy", approvalPolicy,
		"sandbox_mode", sandboxMode,
		"reasoning_effort", reasoningEffort,
		"model", model,
		"multi_agent_mode", multiAgentMode,
		"collaboration_mode", request.Collaboration,
	)
	turnResp, err := newEffectRunner(a).RunStartTurn(ctx, application.StartTurn{Frontend: identity.FrontendID(a.FrontendID()), SessionKey: identity.SessionKey(sessionKey), Request: request})
	if err != nil {
		slog.Error("turn/start failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"error", err,
		)
		return "", err
	}
	return turnResp.ID, nil
}

func replyError(a *App, msg *feishu.InboundMessage, err error) error {
	if msg == nil || err == nil {
		return nil
	}
	return newEffectRunner(a).Run(a.Context(), []application.Effect{application.SendMessage{Frontend: identity.FrontendID(a.FrontendID()), Chat: identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)}, ReplyMessageID: msg.MessageID, Text: "执行失败: " + err.Error(), InThread: replyInThreadEnabled(a, msg.ChatType)}})
}

func sendCommandMenu(a *App, msg *feishu.InboundMessage) error {
	card := renderCommandMenuCard(a, makeSessionKey(a, msg))
	return newEffectRunner(a).Run(context.Background(), []application.Effect{application.SendCard{
		Frontend:       identity.FrontendID(a.FrontendID()),
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		View:           feishuoutbound.Card(card),
		InThread:       replyInThreadEnabled(a, msg.ChatType),
	}})
}

func renderCommandMenuCard(a *App, sessionKey string) map[string]any {
	return a.feishu.SimpleStatusCard(planModeTitleForSession(a, sessionKey, "主菜单"), "blue", menuCardBodyForSession(a, sessionKey, "menu.root", "选择功能分组。"), renderRootMenuButtons(configuredBackend(a), sessionKey))
}
