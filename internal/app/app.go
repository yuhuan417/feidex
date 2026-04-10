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
	app := &App{
		cfg:           cfg,
		cfgPath:       cfgPath,
		store:         store,
		codex:         codexClient,
		feishu:        newFeishuClient(cfg.Feishu),
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
		if sess.ActiveTurnID == "" && sess.ActiveSubmissionID == "" && len(sess.Queue) == 0 && len(sess.StagedImages) == 0 && sess.Status == "idle" {
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
		sess.StagedImages = nil
		sess.Status = "idle"
		if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
			clearSessionThreadContext(sess)
		}
		_ = a.store.UpsertSession(sess)
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
	effectiveModel := configuredGlobalModel(a.cfg)
	for _, sess := range a.store.AllSessions() {
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
			_ = a.store.UpsertSession(sess)
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
			if upsertErr := a.store.UpsertSession(sess); upsertErr != nil {
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
			_ = a.store.UpsertSession(sess)
			a.clearSessionLiveThread(sessionKey)
			continue
		}
		setSessionThreadContext(sess, workspaceID, threadResp.Thread.ID, threadResp.Thread.Name, threadResp.Thread.Preview)
		sess.Status = "idle"
		if upsertErr := a.store.UpsertSession(sess); upsertErr != nil {
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
	if msg == nil {
		return
	}
	if a.isStaleInboundMessage(msg) {
		slog.Debug("feishu stale message ignored", "message_id", msg.MessageID, "created_at", msg.CreatedAt)
		return
	}
	if a.deduper != nil && !a.deduper.Claim(msg.MessageID) {
		slog.Debug("feishu duplicate message ignored by inbound deduper", "message_id", msg.MessageID)
		return
	}
	releaseClaim := true
	defer func() {
		if releaseClaim && a.deduper != nil {
			a.deduper.Release(msg.MessageID)
		}
	}()
	markHandled := func() {
		if a.deduper != nil {
			a.deduper.MarkDone(msg.MessageID)
		}
		releaseClaim = false
	}
	sessionKey := a.makeSessionKey(msg)
	logText := truncate(msg.Text, 160)
	if a.shouldRedactInboundText(sessionKey, msg.UserID) {
		logText = "[redacted pending input]"
	}
	slog.Debug("feishu inbound",
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
			return
		}
		markHandled()
		return
	}
	if strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
		if isLocalCommand(strings.TrimSpace(msg.Text)) {
			if err := a.handleCommand(msg, strings.TrimSpace(msg.Text)); err != nil {
				_ = a.replyError(msg, err)
				return
			}
			markHandled()
			return
		}
	}
	replyLink := a.replyRootTurnLink(msg)
	if a.shouldStageInboundImages(msg) {
		if err := a.stageInboundImagesForSession(msg, a.makeSessionKey(msg)); err != nil {
			_ = a.replyError(msg, err)
			return
		}
		markHandled()
		return
	}
	if strings.TrimSpace(msg.Text) == "" && len(msg.Attachments) == 0 {
		markHandled()
		return
	}
	if replyLink != nil {
		if steered, err := a.trySteerInboundReply(msg, replyLink); err == nil && steered {
			markHandled()
			return
		} else if err != nil {
			slog.Warn("reply steer failed; falling back to queue",
				"message_id", msg.MessageID,
				"parent_message_id", msg.ParentMessageID,
				"thread_id", firstNonEmpty(replyLink.ThreadID, ""),
				"turn_id", firstNonEmpty(replyLink.TurnID, ""),
				"error", err,
			)
		}
	}
	if err := a.enqueueSubmissionWithSessionKey(msg, a.makeSessionKey(msg), replyLink != nil); err != nil {
		_ = a.replyError(msg, err)
		return
	}
	markHandled()
}

func (a *App) handleFeishuRecall(recall *feishu.MessageRecall) {
	if recall == nil || strings.TrimSpace(recall.MessageID) == "" {
		return
	}
	if discarded := a.discardPendingInputByMessageID(recall.MessageID); discarded {
		slog.Debug("feishu recall discarded pending input", "message_id", recall.MessageID, "chat_id", recall.ChatID)
	}
}

func (a *App) handleFeishuReaction(reaction *feishu.MessageReaction) {
	if reaction == nil || strings.TrimSpace(reaction.MessageID) == "" {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(reaction.EmojiType), discardReactionEmoji) {
		return
	}
	if discarded := a.discardPendingInputByMessageID(reaction.MessageID); discarded {
		slog.Debug("feishu reaction discarded pending input",
			"message_id", reaction.MessageID,
			"chat_id", reaction.ChatID,
			"user_id", reaction.UserID,
			"emoji_type", reaction.EmojiType,
		)
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
	return a.enqueueSubmissionWithSessionKey(msg, a.makeSessionKey(msg), false)
}

func (a *App) enqueueSubmissionWithSessionKey(msg *feishu.InboundMessage, sessionKey string, bindOnlyCurrentRoot bool) error {
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
	inboundAttachments, err := a.resolveInboundAttachments(msg, sess.WorkspaceID, sessionKey)
	if err != nil {
		return err
	}
	bucketSessionKey := a.pendingInputSessionKey(msg)
	stagedImages := a.collectPendingStagedImages(sessionKey, bucketSessionKey)
	attachments := append(stagedImageAttachments(stagedImages), inboundAttachments...)
	sourceMessageIDs := uniqueStrings(append([]string{msg.MessageID}, stagedImageSourceMessageIDs(stagedImages)...))
	currentRootMessageID := firstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID))
	sourceRootMessageIDs := []string{currentRootMessageID}
	if !bindOnlyCurrentRoot {
		sourceRootMessageIDs = uniqueStrings(append(sourceRootMessageIDs, stagedImageRootMessageIDs(stagedImages)...))
	}
	if sessionHasInFlightSubmission(sess) {
		sess.Status = "queued"
	}
	slog.Debug("submission enqueue begin",
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
		SessionKey:           sessionKey,
		WorkspaceID:          sess.WorkspaceID,
		UserID:               msg.UserID,
		UserName:             msg.UserName,
		ChatID:               msg.ChatID,
		ChatName:             msg.ChatName,
		TriggerMessageID:     msg.MessageID,
		SourceMessageIDs:     sourceMessageIDs,
		SourceRootMessageIDs: sourceRootMessageIDs,
		InputText:            msg.Text,
		Attachments:          attachments,
		Status:               "queued",
	}
	id, err := a.store.CreateSubmission(sub)
	if err != nil {
		return err
	}
	a.recordInboundSubmissionSourceLink(msg.MessageID, sessionKey, id)
	if err := a.store.QueueSubmission(sessionKey, id); err != nil {
		return err
	}
	sub.ID = id
	if len(stagedImages) > 0 {
		if err := a.clearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
			return err
		}
	}
	slog.Debug("submission queued",
		"submission_id", id,
		"session_key", sessionKey,
		"active_thread_id", sess.ActiveThreadID,
		"active_turn_id", sess.ActiveTurnID,
	)
	logSessionState("submission queued session snapshot", sessionKey, a.store.GetSession(sessionKey))
	if !sessionHasInFlightSubmission(sess) {
		slog.Debug("submission starting immediately",
			"submission_id", id,
			"session_key", sessionKey,
		)
		return a.startNextSubmission(sessionKey)
	}
	a.markSubmissionQueuedReactions(sub)
	a.sendSubmissionQueuedNotice(context.Background(), sub)
	return nil
}

func (a *App) startNextSubmission(sessionKey string) error {
	sess := a.store.GetSession(sessionKey)
	logSessionState("startNextSubmission entry", sessionKey, sess)
	if sess == nil || sessionHasInFlightSubmission(sess) {
		slog.Debug("startNextSubmission skipped",
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
		slog.Debug("startNextSubmission no queued item",
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
	slog.Debug("startNextSubmission picked",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"workspace_id", sub.WorkspaceID,
		"cwd", ws.Cwd,
		"thread_id", sess.ActiveThreadID,
	)
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	if !sessionCanResumeThreadForSubmission(sess, sub) {
		if strings.TrimSpace(sess.ActiveThreadID) != "" {
			slog.Debug("dropping session thread lineage for new submission",
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
	if threadID != "" && !a.sessionHasLiveThread(sessionKey, threadID) {
		slog.Warn("dropping non-live session thread before submission",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"workspace_id", sub.WorkspaceID,
		)
		threadID = ""
		clearSessionThreadContext(sess)
	}
	effectiveModel := configuredGlobalModel(a.cfg)
	effectiveReasoningEffort := configuredGlobalReasoningEffort(a.cfg)
	effectiveApprovalPolicy := effectiveThreadApprovalPolicy(sess, ws)
	effectiveSandboxMode := effectiveThreadSandboxMode(sess, ws)
	effectiveServiceTier := effectiveThreadServiceTier(sess)
	if threadID == "" {
		threadParams := a.buildThreadStartParams(ws, sess, effectiveModel)
		var threadResp codexrpc.ThreadStartResult
		slog.Debug("thread start request",
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
		slog.Debug("thread started",
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
	a.notePendingTurnBinding(threadID, sessionKey, sub.ID)
	if err := a.store.UpsertSession(sess); err != nil {
		a.clearPendingTurnBinding(threadID)
		return err
	}
	if err := a.store.UpdateSubmission(sub.ID, func(m *state.Submission) {
		m.ThreadID = threadID
		m.Status = "running"
	}); err != nil {
		a.clearPendingTurnBinding(threadID)
		return err
	}
	a.markSubmissionRunningReactions(sub)
	logSessionState("startNextSubmission session starting", sessionKey, a.store.GetSession(sessionKey))
	turnCtx, turnCancel := context.WithTimeout(context.Background(), 30*time.Second)
	turnID, err := a.startSubmissionTurn(turnCtx, sessionKey, threadID, sub, ws.Cwd, effectiveApprovalPolicy, effectiveSandboxMode, effectiveServiceTier, effectiveModel, effectiveReasoningEffort)
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
		a.clearPendingTurnBinding(threadID)
		a.clearSubmissionProcessingReactions(sub)
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
	slog.Debug("turn started",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	sess.ActiveSubmissionID = sub.ID
	sess.ActiveTurnID = turnID
	sess.Status = "turn_in_progress"
	a.bindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	a.markTurnStartedAt(turnID, time.Now())
	a.clearPendingTurnBinding(threadID)
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
	a.recordSubmissionSourceLinks(sub)
	a.recordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
	a.noteTurnStarted(sessionKey, sub)
	slog.Debug("startNextSubmission completed",
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
	chatIDs := startupReadyChatIDs(a.store.AllSessions())
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
	buttons := []feishu.Button{
		{Text: submenuLabel("会话行为"), Type: "default", Value: map[string]any{"action": "menu.group.session", "session_key": sessionKey}},
		{Text: submenuLabel("会话管理"), Type: "default", Value: map[string]any{"action": "menu.group.context", "session_key": sessionKey}},
		{Text: submenuLabel("模型能力"), Type: "default", Value: map[string]any{"action": "menu.group.model", "session_key": sessionKey}},
		{Text: submenuLabel("服务管理"), Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("命令菜单", "blue", menuCardBody("menu.root", "选择功能分组。"), buttons)
}
