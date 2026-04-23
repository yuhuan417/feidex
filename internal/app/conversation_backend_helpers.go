package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type uiWarningError struct {
	message string
}

func (e uiWarningError) Error() string {
	return e.message
}

func newUIWarningError(message string) error {
	return uiWarningError{message: strings.TrimSpace(message)}
}

func isUIWarningError(err error) bool {
	var target uiWarningError
	return errors.As(err, &target)
}

func (a *App) resumeClaudeSelectedThread(sessionKey string, sess *state.Session, ws *config.Workspace, selection threadResumeSelection) (*workspaceThreadBinding, error) {
	if a == nil || a.claude == nil {
		return nil, fmt.Errorf("claude backend not initialized")
	}
	threadID := strings.TrimSpace(selection.ThreadID)
	if threadID == "" {
		return nil, fmt.Errorf("missing Claude session id")
	}
	selectedName := strings.TrimSpace(selection.Name)
	selectedPreview := strings.TrimSpace(selection.Preview)
	selectedCWD := strings.TrimSpace(selection.Cwd)
	entry, err := a.findClaudeSessionEntry(threadID)
	if err != nil {
		return nil, err
	}
	if entry != nil {
		selectedName = firstNonEmpty(selectedName, strings.TrimSpace(entry.Name))
		selectedPreview = firstNonEmpty(selectedPreview, strings.TrimSpace(entry.Preview))
		selectedCWD = firstNonEmpty(selectedCWD, strings.TrimSpace(entry.Cwd))
	}
	if selectedCWD != "" && !sameWorkspaceCWD(selectedCWD, ws.Cwd) {
		return nil, newUIWarningError("该会话不属于当前工作区，请先切换 workspace")
	}
	model := firstNonEmpty(strings.TrimSpace(sess.ModelOverride), strings.TrimSpace(ws.Model), strings.TrimSpace(a.cfg.Claude.Model))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resumedID, err := a.claude.EnsureSession(ctx, sessionKey, ws, threadID, model)
	if err != nil {
		return nil, err
	}
	clearSessionThreadContext(sess)
	setSessionThreadContext(
		sess,
		firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), a.defaultWorkspaceID()),
		resumedID,
		firstNonEmpty(selectedName, "Claude"),
		firstNonEmpty(selectedPreview, ws.Name),
	)
	sessionResetActiveOperations(sess)
	sess.Status = "idle"
	if err := a.appState().saveSession(sess); err != nil {
		return nil, err
	}
	a.markSessionThreadLive(sessionKey, resumedID)
	return &workspaceThreadBinding{
		ThreadID: resumedID,
		Name:     sess.ActiveThreadName,
		Preview:  sess.ActiveThreadPreview,
		Resumed:  true,
	}, nil
}

func (a *App) resumeCodexSelectedThread(sessionKey string, sess *state.Session, ws *config.Workspace, selection threadResumeSelection) (*workspaceThreadBinding, error) {
	client, err := a.requireCodexClient()
	if err != nil {
		return nil, err
	}
	threadID := strings.TrimSpace(selection.ThreadID)
	if threadID == "" {
		return nil, fmt.Errorf("missing thread id")
	}
	selectedName := strings.TrimSpace(selection.Name)
	selectedPreview := strings.TrimSpace(selection.Preview)
	selectedCWD := strings.TrimSpace(selection.Cwd)
	if selectedCWD != "" && !sameWorkspaceCWD(selectedCWD, ws.Cwd) {
		return nil, newUIWarningError("该线程不属于当前工作区，请先切换 workspace")
	}
	effectiveModel := configuredGlobalModel(a.cfg)
	params := map[string]any{
		"threadId":               threadID,
		"persistExtendedHistory": true,
	}
	if strings.TrimSpace(effectiveModel) != "" {
		params["model"] = effectiveModel
	}
	slog.Debug("manual thread resume request",
		"session_key", sessionKey,
		"thread_id", threadID,
		"model", effectiveModel,
	)
	var result codexrpc.ThreadStartResult
	if err := client.Call(context.Background(), "thread/resume", params, &result); err != nil {
		return nil, err
	}
	boundThreadID := firstNonEmpty(strings.TrimSpace(result.Thread.ID), threadID)
	sess.ActiveThreadApprovalPolicy = ""
	sess.ActiveThreadSandboxMode = ""
	sess.ActiveClaudePermissionMode = ""
	setSessionThreadContext(sess, firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), a.defaultWorkspaceID()), boundThreadID, firstNonEmpty(selectedName, result.Thread.Name), firstNonEmpty(selectedPreview, result.Thread.Preview))
	a.markSessionThreadLive(sessionKey, boundThreadID)
	sessionResetActiveOperations(sess)
	sess.Status = "idle"
	if err := a.appState().saveSession(sess); err != nil {
		return nil, err
	}
	return &workspaceThreadBinding{
		ThreadID: boundThreadID,
		Name:     sess.ActiveThreadName,
		Preview:  sess.ActiveThreadPreview,
		Resumed:  true,
	}, nil
}

func (a *App) interruptClaudeActiveTurn(ctx context.Context, sessionKey string) error {
	if a == nil || a.claude == nil {
		return fmt.Errorf("claude backend not initialized")
	}
	return a.claude.Interrupt(ctx, sessionKey)
}

func (a *App) interruptCodexActiveTurn(ctx context.Context, sess *state.Session) error {
	client, err := a.requireCodexClient()
	if err != nil {
		return err
	}
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveTurnID) == "" {
		return fmt.Errorf("当前没有运行中的任务")
	}
	return client.Call(ctx, "turn/interrupt", map[string]any{
		"threadId": sess.ActiveThreadID,
		"turnId":   sess.ActiveTurnID,
	}, nil)
}

func (a *App) continueCodexActiveTurn(sessionKey, text string) error {
	client, err := a.requireCodexClient()
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	sess := a.appState().session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveTurnID) == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return client.Call(ctx, "turn/steer", map[string]any{
		"threadId":       sess.ActiveThreadID,
		"expectedTurnId": sess.ActiveTurnID,
		"input": []map[string]any{
			{"type": "text", "text": text, "text_elements": []any{}},
		},
	}, nil)
}

func (a *App) tryCodexReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	if a == nil || msg == nil || link == nil {
		return false, nil
	}
	threadID := strings.TrimSpace(link.ThreadID)
	turnID := strings.TrimSpace(link.TurnID)
	if threadID == "" || turnID == "" {
		return false, nil
	}
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
	bucketSessionKey := a.pendingInputSessionKey(msg)
	inboundAttachments, err := a.resolveInboundAttachments(msg, sess.WorkspaceID, sessionKey)
	if err != nil {
		return false, err
	}
	stagedImages := a.collectPendingStagedImages(sessionKey, bucketSessionKey)
	inputSub := &state.Submission{
		InputText:            msg.Text,
		Attachments:          append(stagedImageAttachments(stagedImages), inboundAttachments...),
		WorkspaceID:          sess.WorkspaceID,
		SessionKey:           sessionKey,
		TriggerMessageID:     msg.MessageID,
		SourceRootMessageIDs: uniqueStrings(append([]string{firstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID))}, stagedImageRootMessageIDs(stagedImages)...)),
	}
	inputs := buildTurnInputs(inputSub)
	if len(inputs) == 0 {
		return false, nil
	}
	client, err := a.requireCodexClient()
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.Call(ctx, "turn/steer", map[string]any{
		"threadId":       threadID,
		"expectedTurnId": turnID,
		"input":          inputs,
	}, nil); err != nil {
		return false, err
	}
	sess.WorkspaceID = firstNonEmpty(sess.WorkspaceID, a.defaultWorkspaceID())
	if err := a.appState().saveSession(sess); err != nil {
		return false, err
	}
	if err := a.clearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
		return false, err
	}
	return true, nil
}
