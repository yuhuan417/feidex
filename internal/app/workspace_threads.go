package app

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
)

type workspaceThreadBinding struct {
	ThreadID string
	Name     string
	Preview  string
	Resumed  bool
}

func (s workspaceThreadService) listWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return conversationBackend(s.app).listWorkspaceThreads(sessionKey, ws, includeAll)
}

func (s workspaceThreadService) listCodexWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	client, err := requireCodexClient(s.app)
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
	return filterThreadsByWorkspaceCWD(result.Data, ws.Cwd), nil
}

func sortThreadsByUpdated(items []codexrpc.ThreadListEntry) {
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
}

func (s workspaceThreadService) ensureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return conversationBackend(s.app).ensureWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (s workspaceThreadService) ensureClaudeWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	if s.app == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	if sess == nil {
		return nil, fmt.Errorf("session not initialized")
	}
	if ws == nil {
		return nil, fmt.Errorf("workspace not found")
	}
	if strings.TrimSpace(sess.ActiveThreadWorkspaceID) == strings.TrimSpace(ws.ID) && strings.TrimSpace(sess.ActiveThreadID) != "" {
		model := firstNonEmpty(strings.TrimSpace(sess.ModelOverride), strings.TrimSpace(ws.Model), strings.TrimSpace(s.app.cfg.Claude.Model))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		threadID, err := s.app.claude.EnsureSession(ctx, sessionKey, ws, sess.ActiveThreadID, model)
		if err == nil {
			setSessionThreadContext(sess, ws.ID, threadID, firstNonEmpty(strings.TrimSpace(sess.ActiveThreadName), "Claude"), firstNonEmpty(strings.TrimSpace(sess.ActiveThreadPreview), ws.Name))
			sessionResetActiveOperations(sess)
			sess.Status = "idle"
			if saveErr := appState(s.app).saveSession(sess); saveErr != nil {
				return nil, saveErr
			}
			markSessionThreadLive(s.app, sessionKey, threadID)
			return &workspaceThreadBinding{
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
	return newWorkspaceThreadService(s.app).startClaudeWorkspaceThread(sessionKey, sess, ws)
}

func (s workspaceThreadService) ensureCodexWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	if s.app == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	if sess == nil {
		return nil, fmt.Errorf("session not initialized")
	}
	if ws == nil {
		return nil, fmt.Errorf("workspace not found")
	}
	items, err := newWorkspaceThreadService(s.app).listCodexWorkspaceThreads(sessionKey, ws, false)
	if err != nil {
		slog.Warn("workspace thread list failed; falling back to new thread",
			"session_key", sessionKey,
			"workspace_id", ws.ID,
			"cwd", ws.Cwd,
			"error", err,
		)
	}
	if len(items) > 0 {
		sortThreadsByUpdated(items)
		if binding, resumeErr := newWorkspaceThreadService(s.app).resumeCodexWorkspaceThread(sessionKey, sess, ws, items[0]); resumeErr == nil {
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
	return newWorkspaceThreadService(s.app).startCodexWorkspaceThread(sessionKey, sess, ws)
}

func (s workspaceThreadService) resumeCodexWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace, entry codexrpc.ThreadListEntry) (*workspaceThreadBinding, error) {
	appState := appState(s.app)
	client, err := requireCodexClient(s.app)
	if err != nil {
		return nil, err
	}
	threadID := strings.TrimSpace(entry.ID)
	if threadID == "" {
		return nil, fmt.Errorf("missing thread id")
	}
	effectiveModel := configuredGlobalModel(s.app.cfg)
	params := map[string]any{
		"threadId":               threadID,
		"persistExtendedHistory": true,
	}
	if strings.TrimSpace(effectiveModel) != "" {
		params["model"] = effectiveModel
	}
	var result codexrpc.ThreadStartResult
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Call(ctx, "thread/resume", params, &result); err != nil {
		return nil, err
	}
	boundThreadID := firstNonEmpty(strings.TrimSpace(result.Thread.ID), threadID)
	if boundThreadID == "" {
		return nil, fmt.Errorf("thread/resume returned empty thread id")
	}
	clearSessionThreadContext(sess)
	setSessionThreadContext(
		sess,
		ws.ID,
		boundThreadID,
		firstNonEmpty(strings.TrimSpace(result.Thread.Name), strings.TrimSpace(entry.Name)),
		firstNonEmpty(strings.TrimSpace(result.Thread.Preview), strings.TrimSpace(entry.Preview)),
	)
	sessionResetActiveOperations(sess)
	sess.Status = "idle"
	if err := appState.saveSession(sess); err != nil {
		return nil, err
	}
	markSessionThreadLive(s.app, sessionKey, boundThreadID)
	return &workspaceThreadBinding{
		ThreadID: boundThreadID,
		Name:     sess.ActiveThreadName,
		Preview:  sess.ActiveThreadPreview,
		Resumed:  true,
	}, nil
}

func (s workspaceThreadService) startWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return conversationBackend(s.app).startWorkspaceThread(sessionKey, sess, ws)
}

func (s workspaceThreadService) startClaudeWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	if s.app == nil || s.app.claude == nil {
		return nil, fmt.Errorf("claude backend not initialized")
	}
	_ = s.app.claude.ResetSession(sessionKey)
	model := firstNonEmpty(strings.TrimSpace(sess.ModelOverride), strings.TrimSpace(ws.Model), strings.TrimSpace(s.app.cfg.Claude.Model))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	threadID, err := s.app.claude.EnsureSession(ctx, sessionKey, ws, "", model)
	if err != nil {
		return nil, err
	}
	clearSessionThreadContext(sess)
	setSessionThreadContext(sess, ws.ID, threadID, "Claude", firstNonEmpty(strings.TrimSpace(sess.ActiveThreadPreview), ws.Name))
	sessionResetActiveOperations(sess)
	sess.Status = "idle"
	if err := appState(s.app).saveSession(sess); err != nil {
		return nil, err
	}
	markSessionThreadLive(s.app, sessionKey, threadID)
	return &workspaceThreadBinding{
		ThreadID: threadID,
		Name:     sess.ActiveThreadName,
		Preview:  sess.ActiveThreadPreview,
		Resumed:  false,
	}, nil
}

func (s workspaceThreadService) startCodexWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	client, err := requireCodexClient(s.app)
	if err != nil {
		return nil, err
	}
	appState := appState(s.app)
	effectiveModel := configuredGlobalModel(s.app.cfg)
	threadParams := buildThreadStartParams(s.app, ws, sess, effectiveModel)
	var result codexrpc.ThreadStartResult
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Call(ctx, "thread/start", threadParams, &result); err != nil {
		return nil, err
	}
	threadID := strings.TrimSpace(result.Thread.ID)
	if threadID == "" {
		return nil, fmt.Errorf("thread/start returned empty thread id")
	}
	clearSessionThreadContext(sess)
	setSessionThreadContext(sess, ws.ID, threadID, result.Thread.Name, result.Thread.Preview)
	sessionResetActiveOperations(sess)
	sess.Status = "idle"
	if err := appState.saveSession(sess); err != nil {
		return nil, err
	}
	markSessionThreadLive(s.app, sessionKey, threadID)
	return &workspaceThreadBinding{
		ThreadID: threadID,
		Name:     sess.ActiveThreadName,
		Preview:  sess.ActiveThreadPreview,
		Resumed:  false,
	}, nil
}
