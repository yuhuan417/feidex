package workspacecmd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/app/appcore"
	appbackend "feidex/internal/app/backend"
	appclaudesession "feidex/internal/app/claudesession"
	"feidex/internal/app/modelconfig"
	appthreadview "feidex/internal/app/threadview"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
)

// EnsureWorkspaceThreadBinding dispatches to the Claude or Codex
// implementation based on the configured backend.
func (s *ThreadService) EnsureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error) {
	return appbackend.DriverForApp(s.App).Conversation().EnsureWorkspaceThreadBinding(s, sessionKey, sess, ws)
}

// ListWorkspaceThreads dispatches to the Claude or Codex implementation.
func (s *ThreadService) ListWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return appbackend.DriverForApp(s.App).Conversation().ListWorkspaceThreads(s, sessionKey, ws, includeAll)
}

// ListClaudeWorkspaceThreads lists Claude sessions for a workspace.
func (s *ThreadService) ListClaudeWorkspaceThreads(_ string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return appclaudesession.ListSessions("", ws, includeAll)
}

// ListCodexWorkspaceThreads lists Codex threads for a workspace.
func (s *ThreadService) ListCodexWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	client, err := s.RequireCodexClient()
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, fmt.Errorf("workspace not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	queries := []map[string]any{
		{
			"limit":    8,
			"cwd":      ws.Cwd,
			"archived": false,
		},
		{
			"limit":    8,
			"cwd":      ws.Cwd,
			"archived": false,
		},
		{
			"limit":    8,
			"archived": false,
		},
	}
	if includeAll {
		queries[0]["sourceKinds"] = []string{"appServer", "cli", "vscode", "exec"}
	} else {
		queries[0]["sourceKinds"] = []string{"appServer"}
	}

	var result codexrpc.ThreadListResult
	for idx, params := range queries {
		slog.Debug("thread list query",
			"attempt", idx+1,
			"session_key", sessionKey,
			"params", fmt.Sprintf("%v", params),
		)
		result = codexrpc.ThreadListResult{}
		err = client.Call(ctx, "thread/list", params, &result)
		if err != nil {
			slog.Error("thread list query failed", "attempt", idx+1, "error", err)
			continue
		}
		slog.Debug("thread list query result", "attempt", idx+1, "count", len(result.Data))
		if len(result.Data) > 0 {
			break
		}
	}
	if err != nil && len(result.Data) == 0 {
		return nil, err
	}
	return appthreadview.FilterThreadsByWorkspaceCWD(result.Data, ws.Cwd), nil
}

// EnsureClaudeWorkspaceThreadBinding ensures a Claude thread binding for
// the workspace, resuming an existing session if possible.
func (s *ThreadService) EnsureClaudeWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error) {
	if sess == nil {
		return nil, fmt.Errorf("session not initialized")
	}
	if ws == nil {
		return nil, fmt.Errorf("workspace not found")
	}
	claude, err := s.RequireClaudeCore()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sess.ActiveThreadWorkspaceID) == strings.TrimSpace(ws.ID) && strings.TrimSpace(sess.ActiveThreadID) != "" {
		model := s.effectiveClaudeModel(sess, ws)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		threadID, err := claude.EnsureSession(ctx, sessionKey, ws, sess.ActiveThreadID, model)
		if err == nil {
			s.SetSessionThreadCtx(sess, ws.ID, threadID, appcore.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadName), "Claude"), appcore.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadPreview), ws.Name))
			s.SessionResetActiveOps(sess)
			sess.Status = state.SessionStatusIdle.String()
			if saveErr := s.SaveSession(sess); saveErr != nil {
				return nil, saveErr
			}
			s.MarkSessionThreadLive(sessionKey, threadID)
			return &ThreadBinding{
				ThreadID: threadID,
				Name:     sess.ActiveThreadName,
				Preview:  sess.ActiveThreadPreview,
				Resumed:  true,
			}, nil
		}
		slog.Warn("Claude workspace session resume failed; starting fresh session",
			"session_key", sessionKey,
			"workspace_id", ws.ID,
			"thread_id", sess.ActiveThreadID,
			"cwd", ws.Cwd,
			"error", err,
		)
	}
	return s.StartClaudeWorkspaceThread(sessionKey, sess, ws)
}

// EnsureCodexWorkspaceThreadBinding ensures a Codex thread binding for
// the workspace, resuming an existing thread if possible.
func (s *ThreadService) EnsureCodexWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error) {
	if sess == nil {
		return nil, fmt.Errorf("session not initialized")
	}
	if ws == nil {
		return nil, fmt.Errorf("workspace not found")
	}
	items, err := s.ListCodexWorkspaceThreads(sessionKey, ws, false)
	if err != nil {
		slog.Warn("workspace thread list failed; falling back to new thread",
			"session_key", sessionKey,
			"workspace_id", ws.ID,
			"cwd", ws.Cwd,
			"error", err,
		)
	}
	if len(items) > 0 {
		SortThreadsByUpdated(items)
		if binding, resumeErr := s.ResumeCodexWorkspaceThread(sessionKey, sess, ws, items[0]); resumeErr == nil {
			return binding, nil
		} else {
			slog.Warn("workspace thread resume failed; starting fresh thread",
				"session_key", sessionKey,
				"workspace_id", ws.ID,
				"thread_id", items[0].ID,
				"cwd", ws.Cwd,
				"error", resumeErr,
			)
		}
	}
	return s.StartCodexWorkspaceThread(sessionKey, sess, ws)
}

// ResumeCodexWorkspaceThread resumes an existing Codex thread.
func (s *ThreadService) ResumeCodexWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace, entry codexrpc.ThreadListEntry) (*ThreadBinding, error) {
	client, err := s.RequireCodexClient()
	if err != nil {
		return nil, err
	}
	threadID := strings.TrimSpace(entry.ID)
	if threadID == "" {
		return nil, fmt.Errorf("missing thread id")
	}
	effectiveModel := s.effectiveCodexModel(sess, ws)
	params := codexrpc.ThreadResumeParams{
		ThreadID:               threadID,
		PersistExtendedHistory: true,
		Model:                  strings.TrimSpace(effectiveModel),
	}
	var result codexrpc.ThreadStartResult
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Call(ctx, "thread/resume", params.Map(), &result); err != nil {
		return nil, err
	}
	boundThreadID := appcore.FirstNonEmpty(strings.TrimSpace(result.Thread.ID), threadID)
	if boundThreadID == "" {
		return nil, fmt.Errorf("thread/resume returned empty thread id")
	}
	s.ClearSessionThreadCtx(sess)
	s.SetSessionThreadCtx(
		sess,
		ws.ID,
		boundThreadID,
		appcore.FirstNonEmpty(strings.TrimSpace(result.Thread.Name), strings.TrimSpace(entry.Name)),
		appcore.FirstNonEmpty(strings.TrimSpace(result.Thread.Preview), strings.TrimSpace(entry.Preview)),
	)
	s.SessionResetActiveOps(sess)
	sess.Status = state.SessionStatusIdle.String()
	if err := s.SaveSession(sess); err != nil {
		return nil, err
	}
	s.MarkSessionThreadLive(sessionKey, boundThreadID)
	return &ThreadBinding{
		ThreadID: boundThreadID,
		Name:     sess.ActiveThreadName,
		Preview:  sess.ActiveThreadPreview,
		Resumed:  true,
	}, nil
}

// StartWorkspaceThread dispatches to the Claude or Codex implementation.
func (s *ThreadService) StartWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error) {
	return appbackend.DriverForApp(s.App).Conversation().StartWorkspaceThread(s, sessionKey, sess, ws)
}

// StartClaudeWorkspaceThread starts a new Claude session for a workspace.
func (s *ThreadService) StartClaudeWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error) {
	claude, err := s.RequireClaudeCore()
	if err != nil {
		return nil, err
	}
	_ = claude.ResetSession(sessionKey)
	model := s.effectiveClaudeModel(sess, ws)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	threadID, err := claude.EnsureSession(ctx, sessionKey, ws, "", model)
	if err != nil {
		return nil, err
	}
	s.ClearSessionThreadCtx(sess)
	s.SetSessionThreadCtx(sess, ws.ID, threadID, "Claude", appcore.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadPreview), ws.Name))
	s.SessionResetActiveOps(sess)
	sess.Status = state.SessionStatusIdle.String()
	if err := s.SaveSession(sess); err != nil {
		return nil, err
	}
	s.MarkSessionThreadLive(sessionKey, threadID)
	return &ThreadBinding{
		ThreadID: threadID,
		Name:     sess.ActiveThreadName,
		Preview:  sess.ActiveThreadPreview,
		Resumed:  false,
	}, nil
}

// StartCodexWorkspaceThread starts a new Codex thread for a workspace.
func (s *ThreadService) StartCodexWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*ThreadBinding, error) {
	client, err := s.RequireCodexClient()
	if err != nil {
		return nil, err
	}
	effectiveModel := s.effectiveCodexModel(sess, ws)
	threadParams := s.BuildThreadStartParams(ws, sess, effectiveModel)
	var result codexrpc.ThreadStartResult
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Call(ctx, "thread/start", threadParams.Map(), &result); err != nil {
		return nil, err
	}
	threadID := strings.TrimSpace(result.Thread.ID)
	if threadID == "" {
		return nil, fmt.Errorf("thread/start returned empty thread id")
	}
	s.ClearSessionThreadCtx(sess)
	s.SetSessionThreadCtx(sess, ws.ID, threadID, result.Thread.Name, result.Thread.Preview)
	s.SessionResetActiveOps(sess)
	sess.Status = state.SessionStatusIdle.String()
	if err := s.SaveSession(sess); err != nil {
		return nil, err
	}
	s.MarkSessionThreadLive(sessionKey, threadID)
	return &ThreadBinding{
		ThreadID: threadID,
		Name:     sess.ActiveThreadName,
		Preview:  sess.ActiveThreadPreview,
		Resumed:  false,
	}, nil
}

func (s *ThreadService) effectiveCodexModel(sess *state.Session, ws *config.Workspace) string {
	binding := s.agentBindingForSession(sess)
	profileModel := ""
	if provider, ok := s.App.(interface{ BotProfile() *state.BotProfile }); ok {
		if profile := provider.BotProfile(); profile != nil {
			profileModel = strings.TrimSpace(profile.Model)
		}
	}
	return appcore.FirstNonEmpty(
		strings.TrimSpace(sessionModelOverride(sess)),
		strings.TrimSpace(bindingModelOverride(binding)),
		profileModel,
		modelconfig.ConfiguredGlobalModel(s.App.Config()),
	)
}

func (s *ThreadService) effectiveClaudeModel(sess *state.Session, ws *config.Workspace) string {
	binding := s.agentBindingForSession(sess)
	profileModel := ""
	if provider, ok := s.App.(interface{ BotProfile() *state.BotProfile }); ok {
		if profile := provider.BotProfile(); profile != nil {
			profileModel = strings.TrimSpace(profile.ClaudeModel)
		}
	}
	return appcore.FirstNonEmpty(
		strings.TrimSpace(sessionModelOverride(sess)),
		strings.TrimSpace(bindingModelOverride(binding)),
		profileModel,
		strings.TrimSpace(s.App.Config().Claude.Model),
	)
}

func (s *ThreadService) agentBindingForSession(sess *state.Session) *state.AgentBinding {
	if s == nil || s.App == nil || s.App.Store() == nil || sess == nil {
		return nil
	}
	bindingID := strings.TrimSpace(sess.BindingID)
	if bindingID == "" {
		return nil
	}
	if binding := s.App.Store().GetScopedAgentBinding(s.App.FrontendID(), bindingID); binding != nil {
		return binding
	}
	if appcore.AllowLegacyFrontendFallback(s.App) {
		binding := s.App.Store().GetAgentBinding(bindingID)
		if binding != nil && strings.TrimSpace(binding.FrontendID) == "" {
			return binding
		}
	}
	return nil
}

func sessionModelOverride(sess *state.Session) string {
	if sess == nil {
		return ""
	}
	return sess.ModelOverride
}

func bindingModelOverride(binding *state.AgentBinding) string {
	if binding == nil {
		return ""
	}
	return binding.ModelOverride
}
