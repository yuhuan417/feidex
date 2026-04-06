package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
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
}

func New(cfg *config.Config, cfgPath string) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	store, err := state.Open(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	codexClient := codexrpc.New(cfg.Codex.Command)
	app := &App{
		cfg:     cfg,
		cfgPath: cfgPath,
		store:   store,
		codex:   codexClient,
		feishu:  feishu.New(cfg.Feishu),
	}
	codexClient.SetHandlers(app.handleNotification, app.handleServerRequest)
	app.feishu.SetHandlers(app.handleFeishuMessage, app.handleCardAction, app.handleBotMenu)
	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	if err := a.codex.Start(ctx, a.cfg.Codex.ExperimentalAPI); err != nil {
		return err
	}
	a.recoverRuntimeState()
	return a.feishu.Start(ctx)
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
		_ = a.store.UpsertSession(sess)
		cleared++
	}
	if cleared > 0 {
		slog.Info("runtime session state recovery complete", "cleared_sessions", cleared)
	}
}

func (a *App) handleFeishuMessage(msg *feishu.InboundMessage) {
	if msg == nil {
		return
	}
	slog.Info("feishu inbound",
		"message_id", msg.MessageID,
		"chat_id", msg.ChatID,
		"chat_type", msg.ChatType,
		"user_id", msg.UserID,
		"root_message_id", msg.RootMessageID,
		"text", truncate(msg.Text, 160),
	)
	if strings.TrimSpace(msg.Text) == "/" {
		if err := a.sendCommandMenu(msg); err != nil {
			slog.Error("send command menu", "error", err)
		}
		return
	}
	if strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
		if err := a.handleCommand(msg, strings.TrimSpace(msg.Text)); err != nil {
			_ = a.replyError(msg, err)
		}
		return
	}
	if err := a.enqueueSubmission(msg); err != nil {
		_ = a.replyError(msg, err)
	}
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
	if sess.ActiveTurnID != "" {
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
	sub := &state.Submission{
		SessionKey:       sessionKey,
		WorkspaceID:      sess.WorkspaceID,
		UserID:           msg.UserID,
		UserName:         msg.UserName,
		ChatID:           msg.ChatID,
		ChatName:         msg.ChatName,
		TriggerMessageID: msg.MessageID,
		InputText:        msg.Text,
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
	if err := a.sendStatusCardForSubmission(sub, msg, "queued"); err != nil {
		slog.Error("send queued status card", "error", err, "submission", id)
	}
	if sess.ActiveTurnID == "" {
		slog.Info("submission starting immediately",
			"submission_id", id,
			"session_key", sessionKey,
		)
		return a.startNextSubmission(sessionKey)
	}
	return nil
}

func (a *App) startNextSubmission(sessionKey string) error {
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID != "" {
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
		return err
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if threadID != "" {
		var resumeResp codexrpc.ThreadStartResult
		if err := a.codex.Call(ctx, "thread/resume", map[string]any{
			"threadId":               threadID,
			"persistExtendedHistory": true,
		}, &resumeResp); err != nil {
			slog.Warn("thread/resume failed; starting fresh thread",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"thread_id", threadID,
				"error", err,
			)
			threadID = ""
			sess.ActiveThreadID = ""
		} else {
			slog.Info("thread resumed",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"thread_id", threadID,
			)
		}
	}
	if threadID == "" {
		threadParams := map[string]any{
			"cwd":                    ws.Cwd,
			"approvalPolicy":         ws.ApprovalPolicy,
			"sandbox":                ws.SandboxMode,
			"serviceName":            a.cfg.Codex.ServiceName,
			"experimentalRawEvents":  false,
			"persistExtendedHistory": true,
		}
		if strings.TrimSpace(ws.Model) != "" {
			threadParams["model"] = ws.Model
		}
		var threadResp codexrpc.ThreadStartResult
		if err := a.codex.Call(ctx, "thread/start", threadParams, &threadResp); err != nil {
			slog.Error("thread/start failed",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"error", err,
			)
			return err
		}
		threadID = threadResp.Thread.ID
		sess.ActiveThreadID = threadID
		slog.Info("thread started",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
		)
	}
	turnParams := map[string]any{
		"threadId": threadID,
		"input": []map[string]any{
			{"type": "text", "text": sub.InputText, "text_elements": []any{}},
		},
		"cwd":            ws.Cwd,
		"approvalPolicy": ws.ApprovalPolicy,
	}
	if model := firstNonEmpty(sess.ModelOverride, ws.Model, a.defaultModel()); strings.TrimSpace(model) != "" {
		turnParams["model"] = model
	}
	var turnResp codexrpc.TurnStartResult
	if err := a.codex.Call(ctx, "turn/start", turnParams, &turnResp); err != nil {
		slog.Error("turn/start failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"error", err,
		)
		return err
	}
	slog.Info("turn started",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnResp.Turn.ID,
	)
	sess.ActiveSubmissionID = sub.ID
	sess.ActiveTurnID = turnResp.Turn.ID
	sess.Status = "turn_in_progress"
	sub.ThreadID = threadID
	sub.TurnID = turnResp.Turn.ID
	sub.Status = "running"
	if err := a.store.UpsertSession(sess); err != nil {
		return err
	}
	if err := a.store.UpdateSubmission(sub.ID, func(m *state.Submission) {
		m.ThreadID = threadID
		m.TurnID = turnResp.Turn.ID
		m.Status = "running"
	}); err != nil {
		return err
	}
	return a.refreshStatusCard(sub.ID)
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

func (a *App) defaultModel() string {
	for _, ws := range a.cfg.Workspaces {
		if strings.TrimSpace(ws.Model) != "" {
			return ws.Model
		}
	}
	return "gpt-5.4"
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
		{Text: "新会话", Type: "primary", Value: map[string]any{"action": "menu.new", "session_key": a.makeSessionKey(msg)}},
		{Text: "线程列表", Type: "default", Value: map[string]any{"action": "menu.threads", "session_key": a.makeSessionKey(msg)}},
		{Text: "中断任务", Type: "danger", Value: map[string]any{"action": "menu.interrupt", "session_key": a.makeSessionKey(msg)}},
	}
	card := a.feishu.SimpleStatusCard("命令菜单", "blue", "选择一个命令执行。", buttons)
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}
