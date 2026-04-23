package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
)

func (a *App) forkClaudeActiveConversation(sessionKey string, sess *state.Session, ws *config.Workspace) (string, error) {
	if a == nil || a.claude == nil {
		return "", fmt.Errorf("claude backend not initialized")
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), strings.TrimSpace(ws.ID), a.defaultWorkspaceID())
	model := firstNonEmpty(strings.TrimSpace(sess.ModelOverride), strings.TrimSpace(ws.Model), strings.TrimSpace(a.cfg.Claude.Model))
	currentThreadID := strings.TrimSpace(sess.ActiveThreadID)
	currentName := firstNonEmpty(strings.TrimSpace(sess.ActiveThreadName), "Claude")
	currentPreview := firstNonEmpty(strings.TrimSpace(sess.ActiveThreadPreview), ws.Name)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	forkedID, err := a.claude.ForkSession(ctx, sessionKey, ws, currentThreadID, model)
	if err != nil {
		return "", err
	}
	if err := a.persistForkedConversation(sessionKey, sess, workspaceID, forkedID, currentName, currentPreview, true); err != nil {
		return "", err
	}
	return forkedID, nil
}

func (a *App) forkCodexActiveConversation(sessionKey string, sess *state.Session, ws *config.Workspace) (string, error) {
	client, err := a.requireCodexClient()
	if err != nil {
		return "", err
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), strings.TrimSpace(ws.ID), a.defaultWorkspaceID())
	params := map[string]any{
		"threadId":       strings.TrimSpace(sess.ActiveThreadID),
		"cwd":            ws.Cwd,
		"approvalPolicy": effectiveThreadApprovalPolicy(sess, ws),
		"sandbox":        effectiveThreadSandboxMode(sess, ws),
	}
	if serviceTier := effectiveThreadServiceTier(sess); strings.TrimSpace(serviceTier) != "" {
		params["serviceTier"] = strings.TrimSpace(serviceTier)
	}
	if model := configuredGlobalModel(a.cfg); strings.TrimSpace(model) != "" {
		params["model"] = strings.TrimSpace(model)
	}

	var result codexrpc.ThreadStartResult
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.Call(ctx, "thread/fork", params, &result); err != nil {
		return "", err
	}
	forkedID := strings.TrimSpace(result.Thread.ID)
	if forkedID == "" {
		return "", fmt.Errorf("fork thread returned empty thread id")
	}
	if err := a.persistForkedConversation(sessionKey, sess, workspaceID, forkedID, result.Thread.Name, result.Thread.Preview, false); err != nil {
		return "", err
	}
	return forkedID, nil
}

func (a *App) persistForkedConversation(sessionKey string, sess *state.Session, workspaceID, threadID, name, preview string, resetThreadSettings bool) error {
	if sess == nil {
		return fmt.Errorf("session not found")
	}
	if resetThreadSettings {
		clearSessionThreadContext(sess)
	}
	setSessionThreadContext(sess, workspaceID, threadID, name, preview)
	if strings.TrimSpace(threadID) != "" {
		a.markSessionThreadLive(sessionKey, threadID)
	} else {
		a.clearSessionLiveThread(sessionKey)
	}
	sessionResetActiveOperations(sess)
	sess.Status = "idle"
	sess.Queue = nil
	sess.StagedImages = nil
	return a.appState().saveSession(sess)
}
