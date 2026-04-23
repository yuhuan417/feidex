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
	cfg                 *config.Config
	cfgPath             string
	store               *state.Store
	frontendID          string
	frontendConfigIndex int
	backend             string
	codex               codexClient
	claude              claudeCore
	feishu              feishuClient
	started             time.Time
	deduper             *inboundDeduper
	backendSwitchMu     sync.Mutex
	asyncRunner         func(func())
	codexRuntimeMu      sync.Mutex
	codexRecovering     bool
	codexRecoverySource codexClient

	turnStreamsMu     sync.Mutex
	turnStreams       map[string]*turnStream
	turnItemsMu       sync.Mutex
	turnItems         map[string]*turnItemState
	workspaceCloneMu  sync.Mutex
	workspaceCloneOps map[string]*workspaceCloneOperation
	liveThreadMu      sync.Mutex
	liveThreads       map[string]string
	turnBindMu        sync.Mutex
	turnBindings      map[string]turnBinding
	pendingTurns      map[string][]turnBinding
	threadUsage       map[string]codexrpc.ThreadTokenUsage
	claudeUsage       map[string]claudeThreadUsageSnapshot
	finalCardPatchMu  sync.Mutex
	finalCardPatches  map[string]*finalCardPatchState
	skillsMu          sync.Mutex
	pendingSkills     map[string]state.SubmissionSkill
	codexUpgradeMu    sync.Mutex
	codexUpgrade      codexUpgradeSnapshot
	codexRestart      codexRestartSnapshot
	claudeUpgradeMu   sync.Mutex
	claudeUpgrade     claudeUpgradeSnapshot
	claudeRestart     claudeRestartSnapshot
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
		turnStreams:         map[string]*turnStream{},
		turnItems:           map[string]*turnItemState{},
		workspaceCloneOps:   map[string]*workspaceCloneOperation{},
		liveThreads:         map[string]string{},
		turnBindings:        map[string]turnBinding{},
		pendingTurns:        map[string][]turnBinding{},
		threadUsage:         map[string]codexrpc.ThreadTokenUsage{},
		claudeUsage:         map[string]claudeThreadUsageSnapshot{},
		finalCardPatches:    map[string]*finalCardPatchState{},
		pendingSkills:       map[string]state.SubmissionSkill{},
	}
	if backend != "" {
		handle, err := app.buildBackendRuntimeHandle(backend)
		if err != nil {
			return nil, err
		}
		handle.install(app)
	}
	app.feishu.SetHandlers(app.handleFeishuMessage, app.handleCardAction, app.handleBotMenu, app.handleFeishuRecall, app.handleFeishuReaction)
	app.feishu.ConfigureLocalFileLinks("", "")
	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	if err := a.startBackend(ctx); err != nil {
		return err
	}
	a.startInboundDeduperLoop(ctx)
	a.recoverSharedRuntimeState()
	a.recoverFrontendRuntimeState()
	if err := a.startFrontend(ctx); err != nil {
		return err
	}
	a.startDriveArtifactGCLoop(ctx)
	go a.sendStartupReadyNotifications()
	return nil
}

func (a *App) Stop(ctx context.Context) error {
	a.feishu.Stop()
	if a == nil {
		return nil
	}
	return a.currentBackendRuntimeHandle().close()
}

func (a *App) runAsync(fn func()) {
	if fn == nil {
		return
	}
	if a != nil && a.asyncRunner != nil {
		a.asyncRunner(fn)
		return
	}
	go fn()
}

func (a *App) buildThreadStartParams(ws *config.Workspace, sess *state.Session, effectiveModel string) map[string]any {
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

func (a *App) handleFeishuMessage(msg *feishu.InboundMessage) {
	newFeishuEventRouter(a).handleMessage(msg)
}

func (a *App) handleFeishuRecall(recall *feishu.MessageRecall) {
	newFeishuEventRouter(a).handleRecall(recall)
}

func (a *App) handleFeishuReaction(reaction *feishu.MessageReaction) {
	newFeishuEventRouter(a).handleReaction(reaction)
}

func (a *App) isStaleInboundMessage(msg *feishu.InboundMessage) bool {
	if msg == nil || msg.CreatedAt == 0 {
		return false
	}
	return msg.CreatedAt < a.started.Add(-30*time.Second).Unix()
}

func nonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func (a *App) handleBotMenu(click *feishu.BotMenuClick) {
	newFeishuEventRouter(a).handleBotMenu(click)
}

func (a *App) handleCardAction(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	// implemented in actions.go
	return a.dispatchCardAction(action)
}

func (a *App) enqueueSubmission(msg *feishu.InboundMessage) error {
	return a.enqueueSubmissionWithSessionKey(msg, a.makeSessionKey(msg), false)
}

func (a *App) enqueueSubmissionWithSessionKey(msg *feishu.InboundMessage, sessionKey string, bindOnlyCurrentRoot bool) error {
	return newSubmissionWorkflow(a).enqueueSubmissionWithSessionKey(msg, sessionKey, bindOnlyCurrentRoot)
}

func (a *App) startNextSubmission(sessionKey string) error {
	return newSubmissionWorkflow(a).startNextSubmission(sessionKey)
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

func (a *App) startSubmissionTurn(ctx context.Context, sessionKey, threadID string, sub *state.Submission, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort string) (string, error) {
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
	var turnResp codexrpc.TurnStartResult
	if err := a.codex.Call(ctx, "turn/start", turnParams, &turnResp); err != nil {
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

func (a *App) makeSessionKey(msg *feishu.InboundMessage) string {
	if msg != nil && strings.TrimSpace(msg.SessionKey) != "" {
		return a.normalizeSessionKey(msg.SessionKey)
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

func (a *App) defaultWorkspaceID() string {
	if a == nil || a.cfg == nil || len(a.cfg.Workspaces) == 0 {
		return "default"
	}
	return a.cfg.Workspaces[0].ID
}

func (a *App) replyError(msg *feishu.InboundMessage, err error) error {
	if msg == nil || err == nil {
		return nil
	}
	if msg.MessageID != "" {
		return a.feishu.ReplyText(context.Background(), msg.MessageID, "执行失败: "+err.Error(), a.replyInThreadEnabled(msg.ChatType))
	}
	return a.feishu.SendText(context.Background(), msg.ChatID, "执行失败: "+err.Error())
}

func (a *App) sendCommandMenu(msg *feishu.InboundMessage) error {
	card := a.renderCommandMenuCard(a.makeSessionKey(msg))
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
	return err
}

func (a *App) renderCommandMenuCard(sessionKey string) map[string]any {
	return a.feishu.SimpleStatusCard("主菜单", "blue", menuCardBody("menu.root", "选择功能分组。"), renderRootMenuButtons(a.configuredBackend(), sessionKey))
}
