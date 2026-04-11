package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
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
	cfg     *config.Config
	cfgPath string
	store   *state.Store
	codex   codexClient
	feishu  feishuClient
	started time.Time
	deduper *inboundDeduper

	turnStreamsMu sync.Mutex
	turnStreams   map[string]*turnStream
	liveThreadMu  sync.Mutex
	liveThreads   map[string]string
	turnBindMu    sync.Mutex
	turnBindings  map[string]turnBinding
	pendingTurns  map[string]turnBinding
	threadUsage   map[string]codexrpc.ThreadTokenUsage

	statusFlushOnce    sync.Once
	statusFlushMu      sync.Mutex
	statusFlushPending map[string]struct{}
	statusFlushCh      chan struct{}
}

type turnBinding struct {
	SessionKey   string
	SubmissionID string
	ThreadID     string
	StartedAt    time.Time
	FirstFinal   string
	LastUsage    codexrpc.TokenUsageBreakdown
	HasLastUsage bool
}

func New(cfg *config.Config, cfgPath string) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	store, err := state.Open(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	codexClient := newCodexClient(cfg.Codex)
	feishuClient := wrapFeishuClient(newFeishuClient(cfg.Feishu))
	app := &App{
		cfg:           cfg,
		cfgPath:       cfgPath,
		store:         store,
		codex:         codexClient,
		feishu:        feishuClient,
		started:       time.Now(),
		deduper:       newInboundDeduper(),
		turnStreams:   map[string]*turnStream{},
		liveThreads:   map[string]string{},
		turnBindings:  map[string]turnBinding{},
		pendingTurns:  map[string]turnBinding{},
		threadUsage:   map[string]codexrpc.ThreadTokenUsage{},
		statusFlushCh: make(chan struct{}, 1),
	}
	codexClient.SetHandlers(app.handleNotification, app.handleServerRequest)
	app.feishu.SetHandlers(app.handleFeishuMessage, app.handleCardAction, app.handleBotMenu, app.handleFeishuRecall, app.handleFeishuReaction)
	app.feishu.ConfigureMarkdownPreview("", "")
	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	if err := a.codex.Start(ctx, a.cfg.Codex.ExperimentalAPI); err != nil {
		return err
	}
	a.startStatusRefreshLoop(ctx)
	a.startInboundDeduperLoop(ctx)
	a.recoverRuntimeState()
	if err := a.feishu.Start(ctx); err != nil {
		return err
	}
	a.startMarkdownPreviewGCLoop(ctx)
	go a.sendStartupReadyNotifications()
	return nil
}

func (a *App) Stop(ctx context.Context) error {
	a.feishu.Stop()
	return a.codex.Close()
}

func (a *App) recoverRuntimeState() {
	a.resetLiveThreadState()
	appState := a.appState()
	sessions := appState.sessions()
	cleared := 0
	for _, sess := range sessions {
		if strings.TrimSpace(sess.WorkspaceID) == "" {
			sess.WorkspaceID = a.defaultWorkspaceID()
			slog.Warn("repairing empty workspace on startup",
				"session_key", sess.Key,
				"workspace_id", sess.WorkspaceID,
			)
		}
		if sess.ActiveTurnID == "" && sess.ActiveSubmissionID == "" && len(sess.Queue) == 0 && len(sess.StagedImages) == 0 && sess.Status == "idle" {
			if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
				clearSessionThreadContext(sess)
			}
			_ = appState.saveSession(sess)
			continue
		}
		slog.Warn("clearing stale runtime session state on startup",
			"session_key", sess.Key,
			"active_thread_id", sess.ActiveThreadID,
			"active_turn_id", sess.ActiveTurnID,
			"active_submission_id", sess.ActiveSubmissionID,
			"queue_len", len(sess.Queue),
			"status", sess.Status,
		)
		sess.ActiveTurnID = ""
		sess.ActiveSubmissionID = ""
		sess.Queue = nil
		sess.StagedImages = nil
		sess.Status = "idle"
		if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
			clearSessionThreadContext(sess)
		}
		_ = appState.saveSession(sess)
		cleared++
	}
	if cleared > 0 {
		slog.Debug("runtime session state recovery complete", "cleared_sessions", cleared)
	}
	a.expirePendingRequestsOnStartup()
	a.cleanupExpiredAttachments()
	a.recoverSessionThreadsOnStartup()
}

func (a *App) resetLiveThreadState() {
	if a == nil {
		return
	}
	a.liveThreadMu.Lock()
	defer a.liveThreadMu.Unlock()
	a.liveThreads = map[string]string{}
}

func (a *App) recoverSessionThreadsOnStartup() {
	if a == nil || a.store == nil {
		return
	}
	appState := a.appState()
	effectiveModel := configuredGlobalModel(a.cfg)
	for _, sess := range appState.sessions() {
		if sess == nil {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) == "" {
			continue
		}
		if strings.TrimSpace(firstNonEmpty(sess.Status, "idle")) != "idle" {
			continue
		}
		if strings.TrimSpace(sess.ActiveTurnID) != "" || strings.TrimSpace(sess.ActiveSubmissionID) != "" {
			continue
		}
		if len(sess.Queue) != 0 || len(sess.StagedImages) != 0 {
			continue
		}

		sessionKey := strings.TrimSpace(sess.Key)
		workspaceID := firstNonEmpty(sess.ActiveThreadWorkspaceID, sess.WorkspaceID, a.defaultWorkspaceID())
		ws := config.FindWorkspace(a.cfg, workspaceID)
		if ws == nil {
			slog.Warn("startup thread recovery dropped unknown workspace lineage",
				"session_key", sessionKey,
				"thread_id", sess.ActiveThreadID,
				"workspace_id", workspaceID,
			)
			clearSessionThreadContext(sess)
			sess.Status = "idle"
			_ = appState.saveSession(sess)
			a.clearSessionLiveThread(sessionKey)
			continue
		}

		threadID := strings.TrimSpace(sess.ActiveThreadID)
		resumeParams := map[string]any{
			"threadId":               threadID,
			"persistExtendedHistory": true,
		}
		if strings.TrimSpace(effectiveModel) != "" {
			resumeParams["model"] = effectiveModel
		}
		var resumeResp codexrpc.ThreadStartResult
		slog.Debug("startup thread resume request",
			"session_key", sessionKey,
			"thread_id", threadID,
			"workspace_id", workspaceID,
			"model", effectiveModel,
		)
		resumeCtx, resumeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := a.codex.Call(resumeCtx, "thread/resume", resumeParams, &resumeResp)
		resumeCancel()
		if err == nil {
			setSessionThreadContext(sess,
				workspaceID,
				firstNonEmpty(strings.TrimSpace(resumeResp.Thread.ID), threadID),
				firstNonEmpty(strings.TrimSpace(resumeResp.Thread.Name), sess.ActiveThreadName),
				firstNonEmpty(strings.TrimSpace(resumeResp.Thread.Preview), sess.ActiveThreadPreview),
			)
			sess.Status = "idle"
			if upsertErr := appState.saveSession(sess); upsertErr != nil {
				slog.Error("startup thread resume persistence failed",
					"session_key", sessionKey,
					"thread_id", sess.ActiveThreadID,
					"workspace_id", workspaceID,
					"error", upsertErr,
				)
				continue
			}
			a.markSessionThreadLive(sessionKey, sess.ActiveThreadID)
			slog.Debug("startup thread resumed",
				"session_key", sessionKey,
				"thread_id", sess.ActiveThreadID,
				"workspace_id", workspaceID,
				"model", effectiveModel,
			)
			continue
		}

		slog.Warn("startup thread/resume failed; starting fresh thread",
			"session_key", sessionKey,
			"thread_id", threadID,
			"workspace_id", workspaceID,
			"model", effectiveModel,
			"error", err,
		)
		threadParams := a.buildThreadStartParams(ws, sess, effectiveModel)
		var threadResp codexrpc.ThreadStartResult
		slog.Debug("startup thread start request",
			"session_key", sessionKey,
			"workspace_id", workspaceID,
			"cwd", ws.Cwd,
			"model", effectiveModel,
		)
		threadCtx, threadCancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = a.codex.Call(threadCtx, "thread/start", threadParams, &threadResp)
		threadCancel()
		if err != nil {
			slog.Error("startup thread/start failed; clearing thread lineage",
				"session_key", sessionKey,
				"stale_thread_id", threadID,
				"workspace_id", workspaceID,
				"cwd", ws.Cwd,
				"error", err,
			)
			clearSessionThreadContext(sess)
			sess.Status = "idle"
			_ = appState.saveSession(sess)
			a.clearSessionLiveThread(sessionKey)
			continue
		}
		setSessionThreadContext(sess, workspaceID, threadResp.Thread.ID, threadResp.Thread.Name, threadResp.Thread.Preview)
		sess.Status = "idle"
		if upsertErr := appState.saveSession(sess); upsertErr != nil {
			slog.Error("startup fresh thread persistence failed",
				"session_key", sessionKey,
				"thread_id", threadResp.Thread.ID,
				"workspace_id", workspaceID,
				"error", upsertErr,
			)
			continue
		}
		a.markSessionThreadLive(sessionKey, threadResp.Thread.ID)
		slog.Debug("startup thread started",
			"session_key", sessionKey,
			"thread_id", threadResp.Thread.ID,
			"workspace_id", workspaceID,
			"model", effectiveModel,
		)
	}
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
	if msg.ChatType == "group" {
		root := msg.RootMessageID
		if root == "" {
			root = msg.MessageID
		}
		return fmt.Sprintf("feishu:group:%s:root:%s", msg.ChatID, root)
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
	if len(a.cfg.Workspaces) == 0 {
		return "default"
	}
	return a.cfg.Workspaces[0].ID
}

func startupReadyChatIDs(sessions []*state.Session) []string {
	seen := map[string]struct{}{}
	chatIDs := make([]string, 0, len(sessions))
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		chatID := strings.TrimSpace(sess.ChatID)
		if chatID == "" {
			continue
		}
		if _, ok := seen[chatID]; ok {
			continue
		}
		seen[chatID] = struct{}{}
		chatIDs = append(chatIDs, chatID)
	}
	sort.Strings(chatIDs)
	return chatIDs
}

func (a *App) sendStartupReadyNotifications() {
	if a == nil || a.feishu == nil || a.store == nil {
		return
	}
	chatIDs := startupReadyChatIDs(a.appState().sessions())
	if len(chatIDs) == 0 {
		slog.Debug("startup ready notification skipped", "reason", "no_known_chats")
		return
	}
	const text = "feidex 已就绪，可继续发送消息。"
	for _, chatID := range chatIDs {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := a.feishu.SendText(ctx, chatID, text)
		cancel()
		if err != nil {
			slog.Error("startup ready notification failed", "chat_id", chatID, "error", err)
			continue
		}
		slog.Debug("startup ready notification sent", "chat_id", chatID)
	}
}

func (a *App) replyError(msg *feishu.InboundMessage, err error) error {
	if msg == nil || err == nil {
		return nil
	}
	if msg.MessageID != "" {
		return a.feishu.ReplyText(context.Background(), msg.MessageID, "执行失败: "+err.Error(), msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	}
	return a.feishu.SendText(context.Background(), msg.ChatID, "执行失败: "+err.Error())
}

func (a *App) sendCommandMenu(msg *feishu.InboundMessage) error {
	card := a.renderCommandMenuCard(a.makeSessionKey(msg))
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) renderCommandMenuCard(sessionKey string) map[string]any {
	return a.feishu.SimpleStatusCard("主菜单", "blue", menuCardBody("menu.root", "选择功能分组。"), renderRootMenuButtons(sessionKey))
}
