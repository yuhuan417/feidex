package app

import (
	"feidex/internal/application"
	"feidex/internal/domain/identity"
	domainsubmission "feidex/internal/domain/submission"

	"context"
	"feidex/internal/app/appstate"
	"feidex/internal/app/attachments"
	appfeishuwrap "feidex/internal/app/feishuwrap"
	"feidex/internal/domain/conversation"
	appcodexruntime "feidex/internal/runtime/codex"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"feidex/internal/app/backend"
	"feidex/internal/app/goalcmd"
	appautoretry "feidex/internal/runtime/autoretry"

	"feidex/internal/app/serverrequest"

	"feidex/internal/adapter/feishu/turnitem"
	appskillscmd "feidex/internal/app/skillscmd"
	"feidex/internal/runtime/turnbinding"

	appthreadmenu "feidex/internal/app/threadmenu"
	appworkspacecmd "feidex/internal/app/workspacecmd"
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
	codex                  CodexClient
	claude                 ClaudeCore
	feishu                 FeishuClient
	started                time.Time
	frontendRuntime        frontendruntime.FrontendRuntime
	stateMu                sync.Mutex
	stateView              *appstate.Store
	backendConfigMu        sync.Mutex
	composition            *appComposition
	deduper                *frontendruntime.InboundDeduper
	backendSwitchMu        sync.Mutex
	backendStateMu         sync.Mutex
	asyncRunner            func(func())
	waitAsync              func()
	codexRuntimeMu         sync.Mutex
	codexRecovery          *appcodexruntime.RecoveryState
	autoRetries            *appautoretry.Tracker
	frontendRecoveryMu     sync.Mutex
	frontendTrafficMu      sync.Mutex
	frontendMessageTraffic int
	sessionActorsMu        sync.Mutex
	sessionActors          *frontendruntime.SessionActors
	backendSwitching       bool
	backendSwitchTarget    string
	mcp                    *feidexMCPService

	liveThreads *frontendruntime.LiveThreads

	trackers appTrackers
}

// appComposition owns lazily constructed application/backend services. Keeping
// these bindings together prevents the frontend aggregate from becoming a
// second service registry while preserving one cache per frontend runtime.
type appComposition struct {
	mu               sync.Mutex
	threadMenu       *appthreadmenu.Service
	backendConfig    *backendConfigurationService
	backendSelection *backendSelectionService
	backendActions   *backend.ActionService
	serverRequestSvc *serverrequest.Service
	dispatcher       *application.Dispatcher
	effectRunner     *frontendruntime.EffectRunner
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
	finalCardPatches    *finalCardPatchTracker
	pendingSkills       *appskillscmd.PendingSkillTracker
	groupAnnouncements  *groupAnnouncementTracker
	maintenanceTrackers backend.TrackerMap
	goals               *goalcmd.Tracker
}

func New(cfg *config.Config, cfgPath string) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	store, err := state.Open(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	frontends := cfg.ResolvedFrontends()
	if len(frontends) == 0 {
		return nil, fmt.Errorf("no frontend configured")
	}
	return newFrontendApp(cfg, cfgPath, store, frontends[0])
}

func newFrontendApp(cfg *config.Config, cfgPath string, store *state.Store, frontend config.ResolvedFrontend) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	if store == nil {
		return nil, fmt.Errorf("nil store")
	}
	backend := normalizeRuntimeBackend(frontend.Backend)
	FeishuClient := appfeishuwrap.WrapFeishuClient(newFeishuClient(frontend.Feishu))
	app := &App{
		cfg:                 cfg,
		cfgPath:             cfgPath,
		store:               store,
		frontendID:          strings.TrimSpace(frontend.ID),
		frontendConfigIndex: frontend.ConfigIndex,
		backend:             backend,
		backendDriver:       backendDriverForKind(backend),
		composition:         &appComposition{},
		feishu:              FeishuClient,
		started:             time.Now(),
		deduper:             frontendruntime.NewInboundDeduper(),
		liveThreads:         frontendruntime.NewLiveThreads(),
		sessionActors:       frontendruntime.NewSessionActors(),
		autoRetries:         appautoretry.NewTracker(),
		trackers: appTrackers{
			turnStreams:        newTurnStreamTracker(),
			turnItems:          turnitem.NewTracker(),
			workspaceCloneOps:  newWorkspaceCloneTracker(),
			turnBindings:       turnbinding.NewTracker(store),
			finalCardPatches:   newFinalCardPatchTracker(),
			pendingSkills:      appskillscmd.NewPendingSkillTracker(),
			groupAnnouncements: newGroupAnnouncementTracker(),
		},
	}
	app.stateView = appstate.New(app)
	dispatcher := newInputDispatcher(app)
	app.composition.dispatcher = &dispatcher
	effectRunner := newEffectRunner(app)
	app.composition.effectRunner = &effectRunner
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
	a.beginLifecycle(ctx)
	ctx = a.Context()
	if err := startMCPService(a, ctx); err != nil {
		a.frontendRuntime.Cancel()
		return err
	}
	if err := startBackend(a, ctx); err != nil {
		a.frontendRuntime.Cancel()
		_ = stopMCPService(a, context.Background())
		return err
	}
	runAsync(a, func() { a.deduper.RunGC(ctx) })
	recoverSharedRuntimeState(a)
	recoverFrontendRuntimeState(a)
	if err := startFrontend(a, ctx); err != nil {
		a.frontendRuntime.Cancel()
		_ = currentBackendRuntimeHandle(a).close()
		_ = stopMCPService(a, context.Background())
		return err
	}
	newRuntimeMaintenanceService(a).StartDriveArtifactGCLoop(ctx)
	newRuntimeMaintenanceService(a).StartUpgradeCheckLoop(ctx)
	scheduleStartupGroupAnnouncementRefreshes(a)
	go sendStartupReadyNotifications(a)
	runAsync(a, func() { runFeishuAppConfigHeal(a) })
	return nil
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
		effectiveModel = modelConfigSnapshot(a, sess, backendCodex).Model
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
	if err := newSubmissionCoordinator(a).enqueueSubmissionWithSessionKey(msg, sessionKey, bindOnlyCurrentRoot); err != nil {
		return err
	}
	invalidateCodexPlanModeExitArtifactsForSession(a, sessionKey, "当前已有新的提交，旧的计划确认已失效。")
	return nil
}

func startNextSubmission(a *App, sessionKey string) error {
	return newSubmissionQueueServiceFromApp(a).StartNextSubmission(sessionKey)
}

func buildTurnSandboxPolicy(mode string) map[string]any {
	switch strings.TrimSpace(mode) {
	case "read-only":
		return map[string]any{"type": "readOnly"}
	case "workspace-write":
		return map[string]any{"type": "workspaceWrite"}
	case "danger-full-access":
		return map[string]any{"type": "dangerFullAccess"}
	default:
		return nil
	}
}

func startSubmissionTurn(a *App, ctx context.Context, sessionKey, threadID string, sub *domainsubmission.Submission, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort, multiAgentMode string) (string, error) {
	if sub == nil {
		return "", fmt.Errorf("nil submission")
	}
	turnParams := map[string]any{
		"threadId":       threadID,
		"input":          attachments.BuildTurnInputs(sub),
		"cwd":            cwd,
		"approvalPolicy": approvalPolicy,
	}
	if len(turnParams["input"].([]map[string]any)) == 0 {
		return "", fmt.Errorf("submission %q has no input", sub.ID)
	}
	if strings.TrimSpace(model) != "" {
		turnParams["model"] = model
	}
	if strings.TrimSpace(reasoningEffort) != "" {
		turnParams["effort"] = reasoningEffort
	}
	if sandboxPolicy := buildTurnSandboxPolicy(sandboxMode); sandboxPolicy != nil {
		turnParams["sandboxPolicy"] = sandboxPolicy
	}
	if strings.TrimSpace(serviceTier) != "" {
		turnParams["serviceTier"] = strings.TrimSpace(serviceTier)
	}
	if strings.TrimSpace(multiAgentMode) != "" {
		turnParams["multiAgentMode"] = strings.TrimSpace(multiAgentMode)
	}
	if snapshot := sub.ModelConfig; snapshot.Valid {
		if snapshot.CollaborationMode != "" {
			selectedModel, selectedEffort := snapshot.Model, snapshot.Effort
			if snapshot.CollaborationMode == "plan" {
				selectedModel, selectedEffort = snapshot.PlanModel, snapshot.PlanEffort
			}
			if selectedModel != "" {
				turnParams["collaborationMode"] = codexCollaborationModeFromState(&conversation.SessionCollaborationMode{
					Mode: snapshot.CollaborationMode, Model: selectedModel, ReasoningEffort: selectedEffort,
				})
			}
		}
	} else if collaborationMode := codexCollaborationModeForTurnStart(a, sessionKey, threadID); collaborationMode != nil {
		turnParams["collaborationMode"] = collaborationMode
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
		"collaboration_mode", turnParams["collaborationMode"],
	)
	client, err := requireCodexClient(a)
	if err != nil {
		return "", err
	}
	var turnResp codexrpc.TurnStartResult
	if err := client.Call(ctx, "turn/start", turnParams, &turnResp); err != nil {
		slog.Error("turn/start failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"error", err,
		)
		return "", err
	}
	return turnResp.Turn.ID, nil
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
		View:           card,
		InThread:       replyInThreadEnabled(a, msg.ChatType),
	}})
}

func renderCommandMenuCard(a *App, sessionKey string) map[string]any {
	return a.feishu.SimpleStatusCard(planModeTitleForSession(a, sessionKey, "主菜单"), "blue", menuCardBodyForSession(a, sessionKey, "menu.root", "选择功能分组。"), renderRootMenuButtons(configuredBackend(a), sessionKey))
}
