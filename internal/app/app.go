package app

import (
	"context"
	"errors"
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
	codex   *codexrpc.Client
	feishu  *feishu.Adapter
	started time.Time

	turnStreamsMu sync.Mutex
	turnStreams   map[string]*turnStream
	liveThreadMu  sync.Mutex
	liveThreads   map[string]string

	statusFlushOnce    sync.Once
	statusFlushMu      sync.Mutex
	statusFlushPending map[string]struct{}
	statusFlushCh      chan struct{}
}

func New(cfg *config.Config, cfgPath string) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	store, err := state.Open(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	codexClient := codexrpc.New(cfg.Codex)
	app := &App{
		cfg:           cfg,
		cfgPath:       cfgPath,
		store:         store,
		codex:         codexClient,
		feishu:        feishu.New(cfg.Feishu),
		started:       time.Now(),
		turnStreams:   map[string]*turnStream{},
		liveThreads:   map[string]string{},
		statusFlushCh: make(chan struct{}, 1),
	}
	codexClient.SetHandlers(app.handleNotification, app.handleServerRequest)
	app.feishu.SetHandlers(app.handleFeishuMessage, app.handleCardAction, app.handleBotMenu)
	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	if err := a.codex.Start(ctx, a.cfg.Codex.ExperimentalAPI); err != nil {
		return err
	}
	a.startStatusRefreshLoop(ctx)
	a.recoverRuntimeState()
	if err := a.feishu.Start(ctx); err != nil {
		return err
	}
	go a.sendStartupReadyNotifications()
	return nil
}

func (a *App) Stop(ctx context.Context) error {
	a.feishu.Stop()
	return a.codex.Close()
}

func (a *App) recoverRuntimeState() {
	sessions := a.store.AllSessions()
	cleared := 0
	for _, sess := range sessions {
		if strings.TrimSpace(sess.WorkspaceID) == "" {
			sess.WorkspaceID = a.defaultWorkspaceID()
			slog.Warn("repairing empty workspace on startup",
				"session_key", sess.Key,
				"workspace_id", sess.WorkspaceID,
			)
		}
		if sess.ActiveTurnID == "" && sess.ActiveSubmissionID == "" && len(sess.Queue) == 0 && sess.Status == "idle" {
			if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
				clearSessionThreadContext(sess)
			}
			_ = a.store.UpsertSession(sess)
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
		sess.Status = "idle"
		if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
			clearSessionThreadContext(sess)
		}
		_ = a.store.UpsertSession(sess)
		cleared++
	}
	if cleared > 0 {
		slog.Info("runtime session state recovery complete", "cleared_sessions", cleared)
	}
	a.expirePendingRequestsOnStartup()
	_ = a.store.CleanupInboundSeen(time.Now().Add(-24 * time.Hour).Unix())
	a.cleanupExpiredAttachments()
}

func (a *App) handleFeishuMessage(msg *feishu.InboundMessage) {
	if msg == nil {
		return
	}
	if a.isStaleInboundMessage(msg) {
		slog.Info("feishu stale message ignored", "message_id", msg.MessageID, "created_at", msg.CreatedAt)
		return
	}
	if duplicate, err := a.store.MarkInboundSeen(msg.MessageID, nonZero(msg.CreatedAt, time.Now().Unix())); err == nil && duplicate {
		slog.Info("feishu duplicate message ignored by persistent store", "message_id", msg.MessageID)
		return
	} else if err != nil {
		slog.Error("mark inbound seen", "message_id", msg.MessageID, "error", err)
	}
	sessionKey := a.makeSessionKey(msg)
	logText := truncate(msg.Text, 160)
	if a.shouldRedactInboundText(sessionKey, msg.UserID) {
		logText = "[redacted pending input]"
	}
	slog.Info("feishu inbound",
		"message_id", msg.MessageID,
		"chat_id", msg.ChatID,
		"chat_type", msg.ChatType,
		"user_id", msg.UserID,
		"root_message_id", msg.RootMessageID,
		"text", logText,
		"attachment_count", len(msg.Attachments),
	)
	if pending := a.pendingTextRequest(sessionKey, msg.UserID); pending != nil && !strings.HasPrefix(strings.TrimSpace(msg.Text), "/") && len(msg.Attachments) == 0 {
		if err := a.handlePendingTextResponse(msg, pending); err != nil {
			_ = a.replyError(msg, err)
		}
		return
	}
	if strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
		if isLocalCommand(strings.TrimSpace(msg.Text)) {
			if err := a.handleCommand(msg, strings.TrimSpace(msg.Text)); err != nil {
				_ = a.replyError(msg, err)
			}
			return
		}
	}
	if strings.TrimSpace(msg.Text) == "" && len(msg.Attachments) == 0 {
		return
	}
	if err := a.enqueueSubmission(msg); err != nil {
		_ = a.replyError(msg, err)
	}
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
	if click == nil {
		return
	}
	msg := &feishu.InboundMessage{
		UserID:   click.UserID,
		ChatID:   click.UserID,
		ChatType: "p2p",
		Text:     click.Command,
	}
	if err := a.handleCommand(msg, click.Command); err != nil {
		_ = a.feishu.SendText(context.Background(), click.UserID, "命令执行失败: "+err.Error())
	}
}

func (a *App) handleCardAction(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	// implemented in actions.go
	return a.dispatchCardAction(action)
}

func (a *App) enqueueSubmission(msg *feishu.InboundMessage) error {
	sessionKey := a.makeSessionKey(msg)
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{
			Key:           sessionKey,
			WorkspaceID:   a.defaultWorkspaceID(),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        "idle",
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = a.defaultWorkspaceID()
	}
	attachments, err := a.resolveInboundAttachments(msg, sess.WorkspaceID, sessionKey)
	if err != nil {
		return err
	}
	if sessionHasInFlightSubmission(sess) {
		sess.Status = "queued"
	}
	slog.Info("submission enqueue begin",
		"session_key", sessionKey,
		"chat_id", msg.ChatID,
		"user_id", msg.UserID,
		"workspace_id", sess.WorkspaceID,
		"active_thread_id", sess.ActiveThreadID,
		"active_turn_id", sess.ActiveTurnID,
		"queue_len", len(sess.Queue),
	)
	if err := a.store.UpsertSession(sess); err != nil {
		return err
	}
	logSessionState("submission enqueue session persisted", sessionKey, a.store.GetSession(sessionKey))
	sub := &state.Submission{
		SessionKey:       sessionKey,
		WorkspaceID:      sess.WorkspaceID,
		UserID:           msg.UserID,
		UserName:         msg.UserName,
		ChatID:           msg.ChatID,
		ChatName:         msg.ChatName,
		TriggerMessageID: msg.MessageID,
		InputText:        msg.Text,
		Attachments:      attachments,
		Status:           "queued",
	}
	id, err := a.store.CreateSubmission(sub)
	if err != nil {
		return err
	}
	if err := a.store.QueueSubmission(sessionKey, id); err != nil {
		return err
	}
	sub.ID = id
	slog.Info("submission queued",
		"submission_id", id,
		"session_key", sessionKey,
		"active_thread_id", sess.ActiveThreadID,
		"active_turn_id", sess.ActiveTurnID,
	)
	logSessionState("submission queued session snapshot", sessionKey, a.store.GetSession(sessionKey))
	if !sessionHasInFlightSubmission(sess) {
		slog.Info("submission starting immediately",
			"submission_id", id,
			"session_key", sessionKey,
		)
		return a.startNextSubmission(sessionKey)
	}
	a.sendSubmissionQueuedNotice(context.Background(), sub)
	return nil
}

func (a *App) startNextSubmission(sessionKey string) error {
	sess := a.store.GetSession(sessionKey)
	logSessionState("startNextSubmission entry", sessionKey, sess)
	if sess == nil || sessionHasInFlightSubmission(sess) {
		slog.Info("startNextSubmission skipped",
			"session_key", sessionKey,
			"has_session", sess != nil,
			"active_turn_id", func() string {
				if sess == nil {
					return ""
				}
				return sess.ActiveTurnID
			}(),
		)
		return nil
	}
	subID, err := a.store.DequeueSubmission(sessionKey)
	if err != nil || subID == "" {
		slog.Info("startNextSubmission no queued item",
			"session_key", sessionKey,
			"error", err,
		)
		logSessionState("startNextSubmission empty-after-dequeue", sessionKey, a.store.GetSession(sessionKey))
		return err
	}
	logSessionState("startNextSubmission after dequeue", sessionKey, a.store.GetSession(sessionKey))
	sub := a.store.GetSubmission(subID)
	if sub == nil {
		slog.Warn("queued submission missing",
			"session_key", sessionKey,
			"submission_id", subID,
		)
		return nil
	}
	ws := config.FindWorkspace(a.cfg, sub.WorkspaceID)
	if ws == nil {
		slog.Error("workspace resolution failed",
			"submission_id", sub.ID,
			"workspace_id", sub.WorkspaceID,
			"default_workspace_id", a.defaultWorkspaceID(),
		)
		return fmt.Errorf("workspace %q not found", sub.WorkspaceID)
	}
	// Refresh the session after dequeue so we don't write a stale queue back.
	sess = a.store.GetSession(sessionKey)
	if sess == nil {
		return fmt.Errorf("session %q disappeared after dequeue", sessionKey)
	}
	slog.Info("startNextSubmission picked",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"workspace_id", sub.WorkspaceID,
		"cwd", ws.Cwd,
		"thread_id", sess.ActiveThreadID,
	)
	threadID := sess.ActiveThreadID
	if !sessionCanResumeThreadForSubmission(sess, sub) {
		if strings.TrimSpace(sess.ActiveThreadID) != "" {
			slog.Info("dropping session thread lineage for new submission",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"submission_workspace_id", sub.WorkspaceID,
				"active_thread_id", sess.ActiveThreadID,
				"active_thread_workspace_id", sess.ActiveThreadWorkspaceID,
			)
		}
		threadID = ""
		clearSessionThreadContext(sess)
	}
	effectiveModel := configuredGlobalModel(a.cfg)
	effectiveReasoningEffort := configuredGlobalReasoningEffort(a.cfg)
	effectiveApprovalPolicy := effectiveThreadApprovalPolicy(sess, ws)
	effectiveSandboxMode := effectiveThreadSandboxMode(sess, ws)
	threadIsLive := a.sessionHasLiveThread(sessionKey, threadID)
	if threadID != "" && threadIsLive {
		slog.Info("using live thread without resume",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"workspace_id", sub.WorkspaceID,
		)
	}
	if threadID != "" && !threadIsLive {
		resumeParams := map[string]any{
			"threadId":               threadID,
			"persistExtendedHistory": true,
		}
		if strings.TrimSpace(effectiveModel) != "" {
			resumeParams["model"] = effectiveModel
		}
		var resumeResp codexrpc.ThreadStartResult
		slog.Info("thread resume request",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"model", effectiveModel,
		)
		resumeCtx, resumeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := a.codex.Call(resumeCtx, "thread/resume", resumeParams, &resumeResp)
		resumeCancel()
		if err != nil {
			slog.Warn("thread/resume failed; starting fresh thread",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"thread_id", threadID,
				"model", effectiveModel,
				"error", err,
			)
			threadID = ""
			clearSessionThreadContext(sess)
		} else {
			slog.Info("thread resumed",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"thread_id", threadID,
				"model", effectiveModel,
			)
			if strings.TrimSpace(resumeResp.Thread.Name) != "" {
				sess.ActiveThreadName = resumeResp.Thread.Name
			}
			if strings.TrimSpace(resumeResp.Thread.Preview) != "" {
				sess.ActiveThreadPreview = resumeResp.Thread.Preview
			}
		}
	}
	if threadID == "" {
		threadParams := map[string]any{
			"cwd":                    ws.Cwd,
			"approvalPolicy":         effectiveApprovalPolicy,
			"sandbox":                effectiveSandboxMode,
			"serviceName":            a.cfg.Codex.ServiceName,
			"experimentalRawEvents":  false,
			"persistExtendedHistory": true,
		}
		if strings.TrimSpace(effectiveModel) != "" {
			threadParams["model"] = effectiveModel
		}
		var threadResp codexrpc.ThreadStartResult
		slog.Info("thread start request",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"workspace_id", sub.WorkspaceID,
			"cwd", ws.Cwd,
			"model", effectiveModel,
		)
		threadCtx, threadCancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := a.codex.Call(threadCtx, "thread/start", threadParams, &threadResp)
		threadCancel()
		if err != nil {
			slog.Error("thread/start failed",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"workspace_id", sub.WorkspaceID,
				"cwd", ws.Cwd,
				"error", err,
			)
			logSessionState("startNextSubmission thread-start-failed", sessionKey, a.store.GetSession(sessionKey))
			return err
		}
		threadID = threadResp.Thread.ID
		slog.Info("thread started",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"model", effectiveModel,
		)
		setSessionThreadContext(sess, sub.WorkspaceID, threadID, threadResp.Thread.Name, threadResp.Thread.Preview)
		a.markSessionThreadLive(sessionKey, threadID)
	}
	if threadID != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
		setSessionThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
	}
	if threadID != "" {
		a.markSessionThreadLive(sessionKey, threadID)
	}
	sess.ActiveSubmissionID = sub.ID
	sess.Status = "turn_starting"
	sub.ThreadID = threadID
	sub.Status = "running"
	if err := a.store.UpsertSession(sess); err != nil {
		return err
	}
	if err := a.store.UpdateSubmission(sub.ID, func(m *state.Submission) {
		m.ThreadID = threadID
		m.Status = "running"
	}); err != nil {
		return err
	}
	logSessionState("startNextSubmission session starting", sessionKey, a.store.GetSession(sessionKey))
	turnCtx, turnCancel := context.WithTimeout(context.Background(), 30*time.Second)
	turnID, err := a.startSubmissionTurn(turnCtx, sessionKey, threadID, sub, ws.Cwd, effectiveApprovalPolicy, effectiveSandboxMode, effectiveModel, effectiveReasoningEffort)
	turnCancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("turn start timed out; waiting for delayed notification",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"thread_id", threadID,
				"workspace_id", sub.WorkspaceID,
			)
			logSessionState("startNextSubmission awaiting turn-start-notification", sessionKey, a.store.GetSession(sessionKey))
			return nil
		}
		slog.Error("turn start chain failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"workspace_id", sub.WorkspaceID,
			"error", err,
		)
		logSessionState("startNextSubmission turn-start-failed", sessionKey, a.store.GetSession(sessionKey))
		return err
	}
	slog.Info("turn started",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	sess.ActiveSubmissionID = sub.ID
	sess.ActiveTurnID = turnID
	sess.Status = "turn_in_progress"
	sub.ThreadID = threadID
	sub.TurnID = turnID
	sub.Status = "running"
	if err := a.store.UpsertSession(sess); err != nil {
		return err
	}
	logSessionState("startNextSubmission session activated", sessionKey, a.store.GetSession(sessionKey))
	if err := a.store.UpdateSubmission(sub.ID, func(m *state.Submission) {
		m.ThreadID = threadID
		m.TurnID = turnID
		m.Status = "running"
	}); err != nil {
		return err
	}
	a.noteTurnStarted(sessionKey, sub)
	slog.Info("startNextSubmission completed",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	return nil
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

func (a *App) startSubmissionTurn(ctx context.Context, sessionKey, threadID string, sub *state.Submission, cwd, approvalPolicy, sandboxMode, model, reasoningEffort string) (string, error) {
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
	slog.Info("turn start request",
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
	chatIDs := startupReadyChatIDs(a.store.AllSessions())
	if len(chatIDs) == 0 {
		slog.Info("startup ready notification skipped", "reason", "no_known_chats")
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
		slog.Info("startup ready notification sent", "chat_id", chatID)
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
	buttons := []feishu.Button{
		{Text: "/status", Type: "default", Value: map[string]any{"action": "menu.status", "session_key": a.makeSessionKey(msg)}},
		{Text: "/model", Type: "default", Value: map[string]any{"action": "menu.model", "session_key": a.makeSessionKey(msg)}},
		{Text: "/quiet", Type: "default", Value: map[string]any{"action": "menu.quiet", "session_key": a.makeSessionKey(msg)}},
		{Text: "/workspace", Type: "default", Value: map[string]any{"action": "menu.workspace", "session_key": a.makeSessionKey(msg)}},
		{Text: "/threads", Type: "default", Value: map[string]any{"action": "menu.threads", "session_key": a.makeSessionKey(msg)}},
	}
	card := a.feishu.SimpleStatusCard("命令菜单", "blue", "选择命令执行。", buttons)
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}
