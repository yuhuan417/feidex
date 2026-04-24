package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	codex                  codexClient
	claude                 claudeCore
	feishu                 feishuClient
	started                time.Time
	deduper                *inboundDeduper
	backendSwitchMu        sync.Mutex
	backendStateMu         sync.Mutex
	asyncRunner            func(func())
	codexRuntimeMu         sync.Mutex
	codexRecovering        bool
	codexRecoverySource    codexClient
	codexAutoThreadMu      sync.Mutex
	codexAutoThreading     bool
	autoRetries            *autoRetryTracker
	frontendRecoveryMu     sync.Mutex
	frontendTrafficMu      sync.Mutex
	frontendMessageTraffic int
	backendSwitching       bool
	backendSwitchTarget    string

	turnStreams       *turnStreamTracker
	turnItems         *turnItemTracker
	workspaceCloneOps *workspaceCloneTracker
	liveThreads       *liveThreadTracker
	turnBindings      *turnBindingTracker
	finalCardPatches  *finalCardPatchTracker
	pendingSkills     *pendingSkillTracker
	codexMaintenance  *codexMaintenanceTracker
	claudeMaintenance *claudeMaintenanceTracker
}

type turnBinding struct {
	SessionKey             string
	SubmissionID           string
	ThreadID               string
	StartedAt              time.Time
	LastUsage              codexrpc.TokenUsageBreakdown
	HasLastUsage           bool
	ContextUsagePercent    float64
	HasContextUsagePercent bool
}

type claudeThreadUsageSnapshot struct {
	TotalInputTokens         int64
	TotalOutputTokens        int64
	TotalCacheReadTokens     int64
	TotalCacheCreationTokens int64
	TotalCostUSD             float64
	ContextWindow            int64
	ContextUsagePercent      float64
	HasContextUsagePercent   bool
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
	feishuClient := wrapFeishuClient(newFeishuClient(frontend.Feishu))
	app := &App{
		cfg:                 cfg,
		cfgPath:             cfgPath,
		store:               store,
		frontendID:          strings.TrimSpace(frontend.ID),
		frontendConfigIndex: frontend.ConfigIndex,
		backend:             backend,
		feishu:              feishuClient,
		started:             time.Now(),
		deduper:             newInboundDeduper(),
		turnStreams:         newTurnStreamTracker(),
		turnItems:           newTurnItemTracker(),
		workspaceCloneOps:   newWorkspaceCloneTracker(),
		liveThreads:         newLiveThreadTracker(),
		turnBindings:        newTurnBindingTracker(),
		finalCardPatches:    newFinalCardPatchTracker(),
		autoRetries:         newAutoRetryTracker(),
		pendingSkills:       newPendingSkillTracker(),
		codexMaintenance:    newCodexMaintenanceTracker(),
		claudeMaintenance:   newClaudeMaintenanceTracker(),
	}
	if backend != "" {
		handle, err := buildBackendRuntimeHandle(app,backend)
		if err != nil {
			return nil, err
		}
		handle.install(app)
	}
	app.feishu.SetHandlers(
		func(msg *feishu.InboundMessage) { appHandleFeishuMessage(app, msg) },
		func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) { return appHandleCardAction(app, action) },
		func(click *feishu.BotMenuClick) { appHandleBotMenu(app, click) },
		func(recall *feishu.MessageRecall) { appHandleFeishuRecall(app, recall) },
		func(reaction *feishu.MessageReaction) { appHandleFeishuReaction(app, reaction) },
	)
	app.feishu.ConfigureLocalFileLinks("", "")
	return app, nil
}

func appStart(a *App, ctx context.Context) error {
	if err := startBackend(a, ctx); err != nil {
		return err
	}
	startInboundDeduperLoop(a, ctx)
	recoverSharedRuntimeState(a)
	recoverFrontendRuntimeState(a)
	if err := startFrontend(a, ctx); err != nil {
		return err
	}
	newRuntimeMaintenanceService(a).startDriveArtifactGCLoop(ctx)
	go sendStartupReadyNotifications(a)
	return nil
}

func appStop(a *App, ctx context.Context) error {
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

func buildThreadStartParams(a *App, ws *config.Workspace, sess *state.Session, effectiveModel string) map[string]any {
	params := map[string]any{
		"cwd":                    ws.Cwd,
		"approvalPolicy":         effectiveThreadApprovalPolicy(sess, ws),
		"sandbox":                effectiveThreadSandboxMode(sess, ws),
		"serviceName":            a.cfg.Codex.ServiceName,
		"experimentalRawEvents":  false,
		"persistExtendedHistory": true,
	}
	if strings.TrimSpace(effectiveThreadServiceTier(sess)) != "" {
		params["serviceTier"] = strings.TrimSpace(effectiveThreadServiceTier(sess))
	}
	if strings.TrimSpace(effectiveModel) != "" {
		params["model"] = strings.TrimSpace(effectiveModel)
	}
	return params
}

func appHandleFeishuMessage(a *App, msg *feishu.InboundMessage) {
	newFeishuEventRouter(a).handleMessage(msg)
}

func appHandleFeishuRecall(a *App, recall *feishu.MessageRecall) {
	newFeishuEventRouter(a).handleRecall(recall)
}

func appHandleFeishuReaction(a *App, reaction *feishu.MessageReaction) {
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

func appHandleBotMenu(a *App, click *feishu.BotMenuClick) {
	newFeishuEventRouter(a).handleBotMenu(click)
}

func appHandleCardAction(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return dispatchCardAction(a, action)
}

func enqueueSubmission(a *App, msg *feishu.InboundMessage) error {
	return enqueueSubmissionWithSessionKey(a, msg, makeSessionKey(a, msg), false)
}

func enqueueSubmissionWithSessionKey(a *App, msg *feishu.InboundMessage, sessionKey string, bindOnlyCurrentRoot bool) error {
	return newLifecycleCoordinator(a).enqueueSubmissionWithSessionKey(msg, sessionKey, bindOnlyCurrentRoot)
}

func startNextSubmission(a *App, sessionKey string) error {
	return newLifecycleCoordinator(a).startNextSubmission(sessionKey)
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
	slog.Debug("turn start request",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"approval_policy", approvalPolicy,
		"sandbox_mode", sandboxMode,
		"reasoning_effort", reasoningEffort,
		"model", model,
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

func makeSessionKey(a *App, msg *feishu.InboundMessage) string {
	if msg != nil && strings.TrimSpace(msg.SessionKey) != "" {
		return normalizeSessionKey(a, msg.SessionKey)
	}
	frontendID := strings.TrimSpace(a.frontendID)
	if msg.ChatType == "group" {
		root := msg.RootMessageID
		if root == "" {
			root = msg.MessageID
		}
		if frontendID != "" {
			return fmt.Sprintf("feishu:frontend:%s:group:%s:root:%s", frontendID, msg.ChatID, root)
		}
		return fmt.Sprintf("feishu:group:%s:root:%s", msg.ChatID, root)
	}
	if frontendID != "" {
		return fmt.Sprintf("feishu:frontend:%s:p2p:%s:%s", frontendID, msg.ChatID, msg.UserID)
	}
	return fmt.Sprintf("feishu:p2p:%s:%s", msg.ChatID, msg.UserID)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func defaultWorkspaceID(a *App) string {
	if a == nil || a.cfg == nil {
		return "default"
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	if len(a.cfg.Workspaces) == 0 {
		return "default"
	}
	return a.cfg.Workspaces[0].ID
}

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
	return a.feishu.SimpleStatusCard("主菜单", "blue", menuCardBody("menu.root", "选择功能分组。"), renderRootMenuButtons(configuredBackend(a), sessionKey))
}
