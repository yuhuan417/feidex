package convbackend

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appsubmission "feidex/internal/app/submission"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type CodexRPCClient interface {
	Call(ctx context.Context, method string, params any, result any) error
}

type ClaudeSessionClient interface {
	EnsureSession(ctx context.Context, sessionKey string, ws *config.Workspace, resumeThreadID, model string) (string, error)
}

type SessionSaveFunc func(sess *state.Session) error
type SessionLookupFunc func(sessionKey string) *state.Session
type ThreadContextSetter func(sess *state.Session, workspaceID, threadID, name, preview string)

type ClaudeResumeDeps struct {
	FindSessionEntry   func(threadID string) (*codexrpc.ThreadListEntry, error)
	EnsureSession      ClaudeSessionClient
	SaveSession        SessionSaveFunc
	ClearThreadContext func(sess *state.Session)
	SetThreadContext   ThreadContextSetter
	ResetActiveOps     func(sess *state.Session)
	MarkThreadLive     func(sessionKey, threadID string)
	DefaultWorkspaceID func() string
	ResolveModel       func(sess *state.Session, ws *config.Workspace) string
}

func ResumeClaudeSelectedThread(deps ClaudeResumeDeps, sessionKey string, sess *state.Session, ws *config.Workspace, selection ThreadResumeSelection) (*ThreadBinding, error) {
	if deps.EnsureSession == nil {
		return nil, fmt.Errorf("claude backend not initialized")
	}
	threadID := strings.TrimSpace(selection.ThreadID)
	if threadID == "" {
		return nil, fmt.Errorf("missing Claude session id")
	}
	selectedName := strings.TrimSpace(selection.Name)
	selectedPreview := strings.TrimSpace(selection.Preview)
	selectedCWD := strings.TrimSpace(selection.Cwd)
	if deps.FindSessionEntry != nil {
		entry, err := deps.FindSessionEntry(threadID)
		if err != nil {
			return nil, err
		}
		if entry != nil {
			selectedName = firstNonEmpty(selectedName, strings.TrimSpace(entry.Name))
			selectedPreview = firstNonEmpty(selectedPreview, strings.TrimSpace(entry.Preview))
			selectedCWD = firstNonEmpty(selectedCWD, strings.TrimSpace(entry.Cwd))
		}
	}
	if selectedCWD != "" && !sameWorkspaceCWD(selectedCWD, ws.Cwd) {
		return nil, NewUIWarningError("该会话不属于当前工作区，请先切换 workspace")
	}
	model := ""
	if deps.ResolveModel != nil {
		model = strings.TrimSpace(deps.ResolveModel(sess, ws))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resumedID, err := deps.EnsureSession.EnsureSession(ctx, sessionKey, ws, threadID, model)
	if err != nil {
		return nil, err
	}
	if deps.ClearThreadContext != nil {
		deps.ClearThreadContext(sess)
	}
	if deps.SetThreadContext != nil {
		deps.SetThreadContext(
			sess,
			firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), valueOrEmpty(deps.DefaultWorkspaceID)),
			resumedID,
			firstNonEmpty(selectedName, "Claude"),
			firstNonEmpty(selectedPreview, ws.Name),
		)
	}
	if deps.ResetActiveOps != nil {
		deps.ResetActiveOps(sess)
	}
	sess.Status = state.SessionStatusIdle.String()
	if deps.SaveSession != nil {
		if err := deps.SaveSession(sess); err != nil {
			return nil, err
		}
	}
	if deps.MarkThreadLive != nil {
		deps.MarkThreadLive(sessionKey, resumedID)
	}
	return &ThreadBinding{
		ThreadID: resumedID,
		Name:     sess.ActiveThreadName,
		Preview:  sess.ActiveThreadPreview,
		Resumed:  true,
	}, nil
}

type CodexResumeDeps struct {
	RequireClient      func() (CodexRPCClient, error)
	SaveSession        SessionSaveFunc
	SetThreadContext   ThreadContextSetter
	ResetActiveOps     func(sess *state.Session)
	MarkThreadLive     func(sessionKey, threadID string)
	DefaultWorkspaceID func() string
	ConfiguredModel    func() string
}

func ResumeCodexSelectedThread(deps CodexResumeDeps, sessionKey string, sess *state.Session, ws *config.Workspace, selection ThreadResumeSelection) (*ThreadBinding, error) {
	if deps.RequireClient == nil {
		return nil, fmt.Errorf("codex client not initialized")
	}
	client, err := deps.RequireClient()
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
		return nil, NewUIWarningError("该线程不属于当前工作区，请先切换 workspace")
	}
	effectiveModel := ""
	if deps.ConfiguredModel != nil {
		effectiveModel = strings.TrimSpace(deps.ConfiguredModel())
	}
	params := codexrpc.ThreadResumeParams{
		ThreadID:               threadID,
		PersistExtendedHistory: true,
		Model:                  effectiveModel,
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
	sess.ActiveThreadCollaborationMode = nil
	if deps.SetThreadContext != nil {
		deps.SetThreadContext(
			sess,
			firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), valueOrEmpty(deps.DefaultWorkspaceID)),
			boundThreadID,
			firstNonEmpty(selectedName, result.Thread.Name),
			firstNonEmpty(selectedPreview, result.Thread.Preview),
		)
	}
	if deps.MarkThreadLive != nil {
		deps.MarkThreadLive(sessionKey, boundThreadID)
	}
	if deps.ResetActiveOps != nil {
		deps.ResetActiveOps(sess)
	}
	sess.Status = state.SessionStatusIdle.String()
	if deps.SaveSession != nil {
		if err := deps.SaveSession(sess); err != nil {
			return nil, err
		}
	}
	return &ThreadBinding{
		ThreadID: boundThreadID,
		Name:     sess.ActiveThreadName,
		Preview:  sess.ActiveThreadPreview,
		Resumed:  true,
	}, nil
}

type CodexInterruptDeps struct {
	RequireClient func() (CodexRPCClient, error)
}

func InterruptCodexActiveTurn(deps CodexInterruptDeps, ctx context.Context, sess *state.Session) error {
	if deps.RequireClient == nil {
		return fmt.Errorf("codex client not initialized")
	}
	client, err := deps.RequireClient()
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

type CodexContinueDeps struct {
	RequireClient func() (CodexRPCClient, error)
	GetSession    SessionLookupFunc
}

func ContinueCodexActiveTurn(deps CodexContinueDeps, sessionKey, text string) error {
	if deps.RequireClient == nil {
		return fmt.Errorf("codex client not initialized")
	}
	client, err := deps.RequireClient()
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	var sess *state.Session
	if deps.GetSession != nil {
		sess = deps.GetSession(sessionKey)
	}
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

type CodexReplyContinuationDeps struct {
	RequireClient              func() (CodexRPCClient, error)
	ResolveInboundAttachments  func(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]state.SubmissionAttachment, error)
	PendingInputSessionKey     func(msg *feishu.InboundMessage) string
	CollectPendingStagedImages func(sessionKey, bucketSessionKey string) []state.SessionStagedImage
	ClearPendingStagedImages   func(sessionKey, bucketSessionKey string) error
	BuildTurnInputs            func(sub *state.Submission) []map[string]any
	SaveSession                SessionSaveFunc
	DefaultWorkspaceID         func() string
}

func TryCodexReplyContinuation(deps CodexReplyContinuationDeps, msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	if msg == nil || link == nil {
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
			WorkspaceID:   valueOrEmpty(deps.DefaultWorkspaceID),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        state.SessionStatusIdle.String(),
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = valueOrEmpty(deps.DefaultWorkspaceID)
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), valueOrEmpty(deps.DefaultWorkspaceID))
	var inboundAttachments []state.SubmissionAttachment
	var err error
	if deps.ResolveInboundAttachments != nil {
		inboundAttachments, err = deps.ResolveInboundAttachments(msg, workspaceID, sessionKey)
		if err != nil {
			return false, err
		}
	}
	bucketSessionKey := ""
	if deps.PendingInputSessionKey != nil {
		bucketSessionKey = deps.PendingInputSessionKey(msg)
	}
	var stagedImages []state.SessionStagedImage
	if deps.CollectPendingStagedImages != nil {
		stagedImages = deps.CollectPendingStagedImages(sessionKey, bucketSessionKey)
	}
	inputSub := &state.Submission{
		InputText:            msg.Text,
		Attachments:          append(appsubmission.StagedImageAttachments(stagedImages), inboundAttachments...),
		WorkspaceID:          workspaceID,
		SessionKey:           sessionKey,
		TriggerMessageID:     msg.MessageID,
		SourceRootMessageIDs: appsubmission.UniqueStrings(append([]string{firstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID))}, appsubmission.StagedImageRootMessageIDs(stagedImages)...)),
	}
	if deps.BuildTurnInputs == nil {
		return false, nil
	}
	inputs := deps.BuildTurnInputs(inputSub)
	if len(inputs) == 0 {
		return false, nil
	}
	if deps.RequireClient == nil {
		return false, fmt.Errorf("codex client not initialized")
	}
	client, err := deps.RequireClient()
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
	sess.WorkspaceID = firstNonEmpty(sess.WorkspaceID, valueOrEmpty(deps.DefaultWorkspaceID))
	if deps.SaveSession != nil {
		if err := deps.SaveSession(sess); err != nil {
			return false, err
		}
	}
	if deps.ClearPendingStagedImages != nil {
		if err := deps.ClearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
			return false, err
		}
	}
	return true, nil
}

type ClaudeStartupRecoveryDeps struct {
	MarkThreadLive func(sessionKey, threadID string)
}

func RecoverClaudeStartupConversation(deps ClaudeStartupRecoveryDeps, sessionKey, workspaceID string, sess *state.Session) {
	if sess == nil {
		return
	}
	if deps.MarkThreadLive != nil {
		deps.MarkThreadLive(sessionKey, sess.ActiveThreadID)
	}
	slog.Debug("startup Claude session lineage preserved",
		"session_key", sessionKey,
		"thread_id", sess.ActiveThreadID,
		"workspace_id", workspaceID,
	)
}

type CodexStartupRecoveryDeps struct {
	CurrentClient          func() CodexRPCClient
	RuntimeRecovering      func() bool
	BuildThreadStartParams func(ws *config.Workspace, sess *state.Session, effectiveModel string) codexrpc.ThreadStartParams
	SaveSession            SessionSaveFunc
	SetThreadContext       ThreadContextSetter
	ClearThreadContext     func(sess *state.Session)
	MarkThreadLive         func(sessionKey, threadID string)
	ClearSessionLiveThread func(sessionKey string)
}

func RecoverCodexStartupConversation(deps CodexStartupRecoveryDeps, sessionKey, workspaceID string, sess *state.Session, ws *config.Workspace, effectiveModel string) {
	if sess == nil || ws == nil || deps.CurrentClient == nil {
		return
	}
	client := deps.CurrentClient()
	if client == nil {
		return
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	resumeParams := codexrpc.ThreadResumeParams{
		ThreadID:               threadID,
		PersistExtendedHistory: true,
		Model:                  strings.TrimSpace(effectiveModel),
	}
	var resumeResp codexrpc.ThreadStartResult
	slog.Debug("startup thread resume request",
		"session_key", sessionKey,
		"thread_id", threadID,
		"workspace_id", workspaceID,
		"model", effectiveModel,
	)
	resumeCtx, resumeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := client.Call(resumeCtx, "thread/resume", resumeParams.Map(), &resumeResp)
	resumeCancel()
	if err == nil {
		if deps.SetThreadContext != nil {
			deps.SetThreadContext(sess,
				workspaceID,
				firstNonEmpty(strings.TrimSpace(resumeResp.Thread.ID), threadID),
				firstNonEmpty(strings.TrimSpace(resumeResp.Thread.Name), sess.ActiveThreadName),
				firstNonEmpty(strings.TrimSpace(resumeResp.Thread.Preview), sess.ActiveThreadPreview),
			)
		}
		sess.Status = state.SessionStatusIdle.String()
		if deps.SaveSession != nil {
			if upsertErr := deps.SaveSession(sess); upsertErr != nil {
				slog.Error("startup thread resume persistence failed",
					"session_key", sessionKey,
					"thread_id", sess.ActiveThreadID,
					"workspace_id", workspaceID,
					"error", upsertErr,
				)
				return
			}
		}
		if deps.MarkThreadLive != nil {
			deps.MarkThreadLive(sessionKey, sess.ActiveThreadID)
		}
		slog.Debug("startup thread resumed",
			"session_key", sessionKey,
			"thread_id", sess.ActiveThreadID,
			"workspace_id", workspaceID,
			"model", effectiveModel,
		)
		return
	}

	if valueOrFalse(deps.RuntimeRecovering) || deps.CurrentClient() == nil {
		slog.Warn("startup thread recovery deferred while codex runtime recovering",
			"session_key", sessionKey,
			"thread_id", threadID,
			"workspace_id", workspaceID,
			"model", effectiveModel,
			"error", err,
		)
		return
	}

	slog.Warn("startup thread/resume failed; starting fresh thread",
		"session_key", sessionKey,
		"thread_id", threadID,
		"workspace_id", workspaceID,
		"model", effectiveModel,
		"error", err,
	)
	client = deps.CurrentClient()
	if client == nil {
		slog.Warn("startup fresh thread recovery skipped because codex runtime disappeared",
			"session_key", sessionKey,
			"thread_id", threadID,
			"workspace_id", workspaceID,
			"model", effectiveModel,
		)
		return
	}
	if deps.BuildThreadStartParams == nil {
		return
	}
	threadParams := deps.BuildThreadStartParams(ws, sess, effectiveModel)
	var threadResp codexrpc.ThreadStartResult
	slog.Debug("startup thread start request",
		"session_key", sessionKey,
		"workspace_id", workspaceID,
		"cwd", ws.Cwd,
		"model", effectiveModel,
	)
	threadCtx, threadCancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = client.Call(threadCtx, "thread/start", threadParams.Map(), &threadResp)
	threadCancel()
	if err != nil {
		if valueOrFalse(deps.RuntimeRecovering) || deps.CurrentClient() == nil {
			slog.Warn("startup fresh thread recovery deferred while codex runtime recovering",
				"session_key", sessionKey,
				"stale_thread_id", threadID,
				"workspace_id", workspaceID,
				"cwd", ws.Cwd,
				"error", err,
			)
			return
		}
		slog.Error("startup thread/start failed; clearing thread lineage",
			"session_key", sessionKey,
			"stale_thread_id", threadID,
			"workspace_id", workspaceID,
			"cwd", ws.Cwd,
			"error", err,
		)
		if deps.ClearThreadContext != nil {
			deps.ClearThreadContext(sess)
		}
		sess.Status = state.SessionStatusIdle.String()
		if deps.SaveSession != nil {
			_ = deps.SaveSession(sess)
		}
		if deps.ClearSessionLiveThread != nil {
			deps.ClearSessionLiveThread(sessionKey)
		}
		return
	}
	if deps.SetThreadContext != nil {
		deps.SetThreadContext(sess, workspaceID, threadResp.Thread.ID, threadResp.Thread.Name, threadResp.Thread.Preview)
	}
	sess.Status = state.SessionStatusIdle.String()
	if deps.SaveSession != nil {
		if upsertErr := deps.SaveSession(sess); upsertErr != nil {
			slog.Error("startup fresh thread persistence failed",
				"session_key", sessionKey,
				"thread_id", threadResp.Thread.ID,
				"workspace_id", workspaceID,
				"error", upsertErr,
			)
			return
		}
	}
	if deps.MarkThreadLive != nil {
		deps.MarkThreadLive(sessionKey, threadResp.Thread.ID)
	}
	slog.Debug("startup thread started",
		"session_key", sessionKey,
		"thread_id", threadResp.Thread.ID,
		"workspace_id", workspaceID,
		"model", effectiveModel,
	)
}

func valueOrEmpty(fn func() string) string {
	if fn == nil {
		return ""
	}
	return strings.TrimSpace(fn())
}

func valueOrFalse(fn func() bool) bool {
	if fn == nil {
		return false
	}
	return fn()
}
