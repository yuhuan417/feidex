package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
)

func recoverClaudeStartupConversation(a *App, sessionKey, workspaceID string, sess *state.Session) {
	if a == nil || sess == nil {
		return
	}
	markSessionThreadLive(a, sessionKey, sess.ActiveThreadID)
	slog.Debug("startup Claude session lineage preserved",
		"session_key", sessionKey,
		"thread_id", sess.ActiveThreadID,
		"workspace_id", workspaceID,
	)
}

func recoverCodexStartupConversation(a *App, sessionKey, workspaceID string, sess *state.Session, ws *config.Workspace, effectiveModel string) {
	if a == nil || sess == nil || ws == nil {
		return
	}
	client := currentCodexClient(a)
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
		setSessionThreadContext(sess,
			workspaceID,
			firstNonEmpty(strings.TrimSpace(resumeResp.Thread.ID), threadID),
			firstNonEmpty(strings.TrimSpace(resumeResp.Thread.Name), sess.ActiveThreadName),
			firstNonEmpty(strings.TrimSpace(resumeResp.Thread.Preview), sess.ActiveThreadPreview),
		)
		sess.Status = state.SessionStatusIdle.String()
		if upsertErr := a.State().SaveSession(sess); upsertErr != nil {
			slog.Error("startup thread resume persistence failed",
				"session_key", sessionKey,
				"thread_id", sess.ActiveThreadID,
				"workspace_id", workspaceID,
				"error", upsertErr,
			)
			return
		}
		markSessionThreadLive(a, sessionKey, sess.ActiveThreadID)
		slog.Debug("startup thread resumed",
			"session_key", sessionKey,
			"thread_id", sess.ActiveThreadID,
			"workspace_id", workspaceID,
			"model", effectiveModel,
		)
		return
	}

	if codexRuntimeRecovering(a) || currentCodexClient(a) == nil {
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
	client = currentCodexClient(a)
	if client == nil {
		slog.Warn("startup fresh thread recovery skipped because codex runtime disappeared",
			"session_key", sessionKey,
			"thread_id", threadID,
			"workspace_id", workspaceID,
			"model", effectiveModel,
		)
		return
	}
	threadParams := buildThreadStartParams(a, ws, sess, effectiveModel)
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
		if codexRuntimeRecovering(a) || currentCodexClient(a) == nil {
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
		clearSessionThreadContext(sess)
		sess.Status = state.SessionStatusIdle.String()
		_ = a.State().SaveSession(sess)
		clearSessionLiveThread(a, sessionKey)
		return
	}
	setSessionThreadContext(sess, workspaceID, threadResp.Thread.ID, threadResp.Thread.Name, threadResp.Thread.Preview)
	sess.Status = state.SessionStatusIdle.String()
	if upsertErr := a.State().SaveSession(sess); upsertErr != nil {
		slog.Error("startup fresh thread persistence failed",
			"session_key", sessionKey,
			"thread_id", threadResp.Thread.ID,
			"workspace_id", workspaceID,
			"error", upsertErr,
		)
		return
	}
	markSessionThreadLive(a, sessionKey, threadResp.Thread.ID)
	slog.Debug("startup thread started",
		"session_key", sessionKey,
		"thread_id", threadResp.Thread.ID,
		"workspace_id", workspaceID,
		"model", effectiveModel,
	)
}
