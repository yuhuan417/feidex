package codexruntime

import (
	"context"
	codexadapter "feidex/internal/adapter/backend/codex"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/textutil"
	"log/slog"
	"strings"
	"time"
)

type CodexRPCClient interface {
	Call(context.Context, string, any, any) error
}
type SessionSaveFunc func(*conversation.Session) error
type ThreadContextSetter func(*conversation.Session, string, string, string, string)

func firstNonEmpty(values ...string) string { return textutil.FirstNonEmpty(values...) }

type ClaudeStartupRecoveryDeps struct {
	Context        func() context.Context
	MarkThreadLive func(sessionKey, threadID string)
}

func RecoverClaudeStartupConversation(deps ClaudeStartupRecoveryDeps, sessionKey, workspaceID string, sess *conversation.Session) {
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

type StartupRecoveryDeps struct {
	Context                func() context.Context
	CurrentClient          func() CodexRPCClient
	RuntimeRecovering      func() bool
	BuildThreadStartParams func(ws *config.Workspace, sess *conversation.Session, effectiveModel string) codexrpc.ThreadStartParams
	BuildThreadConfig      func(sess *conversation.Session) map[string]any
	SaveSession            SessionSaveFunc
	SetThreadContext       ThreadContextSetter
	ClearThreadContext     func(sess *conversation.Session)
	MarkThreadLive         func(sessionKey, threadID string)
	ClearSessionLiveThread func(sessionKey string)
}

func RecoverStartupConversation(deps StartupRecoveryDeps, sessionKey, workspaceID string, sess *conversation.Session, ws *config.Workspace, effectiveModel string) {
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
	if deps.BuildThreadConfig != nil {
		resumeParams.Config = deps.BuildThreadConfig(sess)
	}
	var resumeResp codexrpc.ThreadStartResult
	slog.Debug("startup thread resume request",
		"session_key", sessionKey,
		"thread_id", threadID,
		"workspace_id", workspaceID,
		"model", effectiveModel,
	)
	resumeCtx, resumeCancel := context.WithTimeout(dependencyContext(deps.Context), 30*time.Second)
	err := client.Call(resumeCtx, "thread/resume", resumeParams.Map(), &resumeResp)
	resumeCancel()
	if err == nil {
		sess.AppliedModelConfig = codexadapter.ResumedThreadConfig(resumeParams.Model, resumeParams.Config)
		sess.ModelConfigError = ""
		if deps.SetThreadContext != nil {
			deps.SetThreadContext(sess,
				workspaceID,
				firstNonEmpty(strings.TrimSpace(resumeResp.Thread.ID), threadID),
				firstNonEmpty(strings.TrimSpace(resumeResp.Thread.Name), sess.ActiveThreadName),
				firstNonEmpty(strings.TrimSpace(resumeResp.Thread.Preview), sess.ActiveThreadPreview),
			)
		}
		sess.Status = conversation.SessionStatusIdle.String()
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

	if valueOrFalse(deps.RuntimeRecovering) || deps.CurrentClient() == nil || deps.CurrentClient() != client {
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
	threadCtx, threadCancel := context.WithTimeout(dependencyContext(deps.Context), 30*time.Second)
	err = client.Call(threadCtx, "thread/start", threadParams.Map(), &threadResp)
	threadCancel()
	if err != nil {
		if valueOrFalse(deps.RuntimeRecovering) || deps.CurrentClient() == nil || deps.CurrentClient() != client {
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
		sess.Status = conversation.SessionStatusIdle.String()
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
	sess.Status = conversation.SessionStatusIdle.String()
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

func valueOrFalse(fn func() bool) bool {
	if fn == nil {
		return false
	}
	return fn()
}

func dependencyContext(get func() context.Context) context.Context {
	if get != nil {
		return get()
	}
	return context.Background()
}
