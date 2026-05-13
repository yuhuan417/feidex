package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"feidex/internal/app/apputil"
	"feidex/internal/app/backend"
	"feidex/internal/app/serverrequest"
	appskillscmd "feidex/internal/app/skillscmd"
	"feidex/internal/app/turnbinding"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
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
	backend                string
	codex                  CodexClient
	claude                 ClaudeCore
	feishu                 FeishuClient
	started                time.Time
	deduper                *inboundDeduper
	backendSwitchMu        sync.Mutex
	backendStateMu         sync.Mutex
	asyncRunner            func(func())
	waitAsync              func()
	codexRuntimeMu         sync.Mutex
	codexRecovering        bool
	codexRecoverySource    CodexClient
	codexAutoThreadMu      sync.Mutex
	codexAutoThreading     bool
	autoRetries            *autoRetryTracker
	frontendRecoveryMu     sync.Mutex
	frontendTrafficMu      sync.Mutex
	frontendMessageTraffic int
	backendSwitching       bool
	backendSwitchTarget    string

	liveThreads *liveThreadTracker

	serverRequestSvc *serverrequest.Service
	trackers         appTrackers
}

// appTrackers bundles per-service runtime trackers that are lazily initialized
// on first access. Each tracker is consumed by exactly one service type.
type appTrackers struct {
	turnStreams         *turnStreamTracker
	turnItems           *turnItemTracker
	turnBindings        *turnbinding.Tracker
	submissionStarts    submissionStartTracker
	workspaceCloneOps   *workspaceCloneTracker
	finalCardPatches    *finalCardPatchTracker
	pendingSkills       *appskillscmd.PendingSkillTracker
	maintenanceTrackers backend.TrackerMap
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
	FeishuClient := wrapFeishuClient(newFeishuClient(frontend.Feishu))
	app := &App{
		cfg:                 cfg,
		cfgPath:             cfgPath,
		store:               store,
		frontendID:          strings.TrimSpace(frontend.ID),
		frontendConfigIndex: frontend.ConfigIndex,
		backend:             backend,
		feishu:              FeishuClient,
		started:             time.Now(),
		deduper:             newInboundDeduper(),
		liveThreads:         newLiveThreadTracker(),
		autoRetries:         newAutoRetryTracker(),
		trackers: appTrackers{
			turnStreams:       newTurnStreamTracker(),
			turnItems:         newTurnItemTracker(),
			workspaceCloneOps: newWorkspaceCloneTracker(),
			turnBindings:      turnbinding.NewTracker(store),
			finalCardPatches:  newFinalCardPatchTracker(),
			pendingSkills:     appskillscmd.NewPendingSkillTracker(),
		},
	}
	if backend != "" {
		handle, err := buildBackendRuntimeHandle(app, backend)
		if err != nil {
			return nil, err
		}
		handle.install(app)
	}
	app.feishu.SetHandlers(app.HandleFeishuMessage, app.HandleCardAction, app.HandleBotMenu, app.HandleFeishuRecall, app.HandleFeishuReaction)
	app.feishu.ConfigureLocalFileLinks("", "")
	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	if err := startBackend(a, ctx); err != nil {
		return err
	}
	startInboundDeduperLoop(a, ctx)
	recoverSharedRuntimeState(a)
	recoverFrontendRuntimeState(a)
	if err := startFrontend(a, ctx); err != nil {
		return err
	}
	newRuntimeMaintenanceService(a).StartDriveArtifactGCLoop(ctx)
	newRuntimeMaintenanceService(a).StartUpgradeCheckLoop(ctx)
	go sendStartupReadyNotifications(a)
	return nil
}

func (a *App) Stop(ctx context.Context) error {
	a.feishu.Stop()
	if a == nil {
		return nil
	}
	return currentBackendRuntimeHandle(a).close()
}

func runAsync(a *App, fn func()) {
	if fn == nil {
		return
	}
	if a != nil && a.asyncRunner != nil {
		a.asyncRunner(fn)
		return
	}
	go fn()
}

func buildThreadStartParams(a *App, ws *config.Workspace, sess *state.Session, effectiveModel string) codexrpc.ThreadStartParams {
	return codexrpc.ThreadStartParams{
		Cwd:                    ws.Cwd,
		ApprovalPolicy:         effectiveThreadApprovalPolicy(sess, ws),
		Sandbox:                effectiveThreadSandboxMode(sess, ws),
		ServiceName:            a.cfg.Codex.ServiceName,
		ExperimentalRawEvents:  false,
		PersistExtendedHistory: true,
		ServiceTier:            strings.TrimSpace(effectiveThreadServiceTier(sess)),
		Model:                  strings.TrimSpace(effectiveModel),
	}
}

func (a *App) HandleFeishuMessage(msg *feishu.InboundMessage) {
	newFeishuEventRouter(a).handleMessage(msg)
}

func (a *App) HandleFeishuRecall(recall *feishu.MessageRecall) {
	newFeishuEventRouter(a).handleRecall(recall)
}

func (a *App) HandleFeishuReaction(reaction *feishu.MessageReaction) {
	newFeishuEventRouter(a).handleReaction(reaction)
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

func (a *App) HandleBotMenu(click *feishu.BotMenuClick) {
	newFeishuEventRouter(a).handleBotMenu(click)
}

func (a *App) HandleCardAction(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return newCardActionService(a).dispatch(action)
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

func startSubmissionTurn(a *App, ctx context.Context, sessionKey, threadID string, sub *state.Submission, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort string) (string, error) {
	if sub == nil {
		return "", fmt.Errorf("nil submission")
	}
	turnParams := map[string]any{
		"threadId":       threadID,
		"input":          buildTurnInputs(sub),
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
	if collaborationMode := codexCollaborationModeForTurnStart(a, sessionKey, threadID); collaborationMode != nil {
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

var firstNonEmpty = apputil.FirstNonEmpty

func replyError(a *App, msg *feishu.InboundMessage, err error) error {
	if msg == nil || err == nil {
		return nil
	}
	if msg.MessageID != "" {
		return a.feishu.ReplyText(context.Background(), msg.MessageID, "执行失败: "+err.Error(), replyInThreadEnabled(a, msg.ChatType))
	}
	return a.feishu.SendText(context.Background(), msg.ChatID, "执行失败: "+err.Error())
}

func sendCommandMenu(a *App, msg *feishu.InboundMessage) error {
	card := renderCommandMenuCard(a, makeSessionKey(a, msg))
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(a, msg.ChatType))
	return err
}

func renderCommandMenuCard(a *App, sessionKey string) map[string]any {
	return a.feishu.SimpleStatusCard(planModeTitleForSession(a, sessionKey, "主菜单"), "blue", menuCardBodyForSession(a, sessionKey, "menu.root", "选择功能分组。"), renderRootMenuButtons(configuredBackend(a), sessionKey))
}
