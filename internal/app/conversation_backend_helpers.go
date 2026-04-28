package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appconvbackend "feidex/internal/app/convbackend"
	appmodelconfig "feidex/internal/app/modelconfig"
	appsessionctx "feidex/internal/app/sessionctx"
	appthreadview "feidex/internal/app/threadview"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// Type aliases — convbackend sub-package
type uiWarningError = appconvbackend.UIWarningError

// Function aliases — convbackend sub-package
var (
	newUIWarningError = appconvbackend.NewUIWarningError
	isUIWarningError  = appconvbackend.IsUIWarningError
)

// Function aliases — other sub-packages used by helpers
var (
	_configuredGlobalModel     = appmodelconfig.ConfiguredGlobalModel
	_clearSessionThreadContext = appsessionctx.ClearThreadContext
	_setSessionThreadContext   = appsessionctx.SetThreadContext
	_sessionResetActiveOps     = appsessionctx.ResetActiveOperations
	_sameWorkspaceCWD          = appthreadview.SameWorkspaceCWD
)

// ---------------------------------------------------------------------------
// Resume helpers — these call app-level services and stay in app/
// ---------------------------------------------------------------------------

func resumeClaudeSelectedThread(a *App, sessionKey string, sess *state.Session, ws *config.Workspace, selection appconvbackend.ThreadResumeSelection) (*workspaceThreadBinding, error) {
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
	entry, err := findClaudeSessionEntry(threadID)
	if err != nil {
		return nil, err
	}
	if entry != nil {
		selectedName = firstNonEmpty(selectedName, strings.TrimSpace(entry.Name))
		selectedPreview = firstNonEmpty(selectedPreview, strings.TrimSpace(entry.Preview))
		selectedCWD = firstNonEmpty(selectedCWD, strings.TrimSpace(entry.Cwd))
	}
	if selectedCWD != "" && !_sameWorkspaceCWD(selectedCWD, ws.Cwd) {
		return nil, newUIWarningError("该会话不属于当前工作区，请先切换 workspace")
	}
	model := firstNonEmpty(strings.TrimSpace(sess.ModelOverride), strings.TrimSpace(ws.Model), strings.TrimSpace(a.cfg.Claude.Model))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resumedID, err := a.claude.EnsureSession(ctx, sessionKey, ws, threadID, model)
	if err != nil {
		return nil, err
	}
	_clearSessionThreadContext(sess)
	_setSessionThreadContext(
		sess,
		firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), defaultWorkspaceID(a)),
		resumedID,
		firstNonEmpty(selectedName, "Claude"),
		firstNonEmpty(selectedPreview, ws.Name),
	)
	_sessionResetActiveOps(sess)
	sess.Status = state.SessionStatusIdle.String()
	if err := a.State().SaveSession(sess); err != nil {
		return nil, err
	}
	markSessionThreadLive(a, sessionKey, resumedID)
	return &workspaceThreadBinding{
		ThreadID: resumedID,
		Name:     sess.ActiveThreadName,
		Preview:  sess.ActiveThreadPreview,
		Resumed:  true,
	}, nil
}

func resumeCodexSelectedThread(a *App, sessionKey string, sess *state.Session, ws *config.Workspace, selection appconvbackend.ThreadResumeSelection) (*workspaceThreadBinding, error) {
	client, err := requireCodexClient(a)
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
	if selectedCWD != "" && !_sameWorkspaceCWD(selectedCWD, ws.Cwd) {
		return nil, newUIWarningError("该线程不属于当前工作区，请先切换 workspace")
	}
	effectiveModel := _configuredGlobalModel(a.cfg)
	params := codexrpc.ThreadResumeParams{
		ThreadID:               threadID,
		PersistExtendedHistory: true,
		Model:                  strings.TrimSpace(effectiveModel),
	}
	slog.Debug("manual thread resume request",
		"session_key", sessionKey,
		"thread_id", threadID,
		"model", effectiveModel,
	)
	var result codexrpc.ThreadStartResult
	if err := client.Call(context.Background(), "thread/resume", params.Map(), &result); err != nil {
		return nil, err
	}
	boundThreadID := firstNonEmpty(strings.TrimSpace(result.Thread.ID), threadID)
	sess.ActiveThreadApprovalPolicy = ""
	sess.ActiveThreadSandboxMode = ""
	sess.ActiveClaudePermissionMode = ""
	_setSessionThreadContext(sess, firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), defaultWorkspaceID(a)), boundThreadID, firstNonEmpty(selectedName, result.Thread.Name), firstNonEmpty(selectedPreview, result.Thread.Preview))
	markSessionThreadLive(a, sessionKey, boundThreadID)
	_sessionResetActiveOps(sess)
	sess.Status = state.SessionStatusIdle.String()
	if err := a.State().SaveSession(sess); err != nil {
		return nil, err
	}
	return &workspaceThreadBinding{
		ThreadID: boundThreadID,
		Name:     sess.ActiveThreadName,
		Preview:  sess.ActiveThreadPreview,
		Resumed:  true,
	}, nil
}

// ---------------------------------------------------------------------------
// Interrupt and continue helpers
// ---------------------------------------------------------------------------

func interruptClaudeActiveTurn(a *App, ctx context.Context, sessionKey string) error {
	if a == nil || a.claude == nil {
		return fmt.Errorf("claude backend not initialized")
	}
	return a.claude.Interrupt(ctx, sessionKey)
}

func interruptCodexActiveTurn(a *App, ctx context.Context, sess *state.Session) error {
	client, err := requireCodexClient(a)
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

func continueCodexActiveTurn(a *App, sessionKey, text string) error {
	client, err := requireCodexClient(a)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	sess := a.State().Session(sessionKey)
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

func tryCodexReplyContinuation(a *App, msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
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
			WorkspaceID:   defaultWorkspaceID(a),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        state.SessionStatusIdle.String(),
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = defaultWorkspaceID(a)
	}
	bucketSessionKey := newReplyContinuationService(a).pendingInputSessionKey(msg)
	inboundAttachments, err := resolveInboundAttachments(a, msg, sess.WorkspaceID, sessionKey)
	if err != nil {
		return false, err
	}
	stagedImages := newReplyContinuationService(a).collectPendingStagedImages(sessionKey, bucketSessionKey)
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
	client, err := requireCodexClient(a)
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
	sess.WorkspaceID = firstNonEmpty(sess.WorkspaceID, defaultWorkspaceID(a))
	if err := a.State().SaveSession(sess); err != nil {
		return false, err
	}
	if err := newReplyContinuationService(a).clearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
		return false, err
	}
	return true, nil
}
