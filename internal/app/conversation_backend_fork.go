package app

import (
	"context"
	"feidex/internal/domain/conversation"
	"feidex/internal/textutil"
	"fmt"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
)

func forkClaudeActiveConversation(a *App, sessionKey string, sess *conversation.Session, ws *config.Workspace) (string, error) {
	if a == nil || a.claude == nil {
		return "", fmt.Errorf("claude backend not initialized")
	}
	workspaceID := textutil.FirstNonEmpty(strings.TrimSpace(sess.WorkspaceID), strings.TrimSpace(ws.ID), defaultWorkspaceID(a))
	model := effectiveClaudeModel(a, sess, ws)
	currentThreadID := strings.TrimSpace(sess.ActiveThreadID)
	currentName := textutil.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadName), "Claude")
	currentPreview := textutil.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadPreview), ws.Name)

	ctx, cancel := context.WithTimeout(a.Context(), 30*time.Second)
	defer cancel()
	forkedID, err := a.claude.ForkSession(ctx, sessionKey, ws, currentThreadID, model)
	if err != nil {
		return "", err
	}
	if err := persistForkedConversation(a, sessionKey, sess, workspaceID, forkedID, currentName, currentPreview, true); err != nil {
		return "", err
	}
	return forkedID, nil
}

func forkCodexActiveConversation(a *App, sessionKey string, sess *conversation.Session, ws *config.Workspace) (string, error) {
	client, err := requireCodexClient(a)
	if err != nil {
		return "", err
	}
	workspaceID := textutil.FirstNonEmpty(strings.TrimSpace(sess.WorkspaceID), strings.TrimSpace(ws.ID), defaultWorkspaceID(a))
	params := map[string]any{
		"threadId":       strings.TrimSpace(sess.ActiveThreadID),
		"cwd":            ws.Cwd,
		"approvalPolicy": effectiveBindingApprovalPolicy(a, sess, ws),
		"sandbox":        effectiveBindingSandboxMode(a, sess, ws),
		"multiAgentMode": effectiveBindingMultiAgentMode(a, sess, ws),
	}
	if serviceTier := effectiveBindingServiceTier(a, sess); strings.TrimSpace(serviceTier) != "" {
		params["serviceTier"] = strings.TrimSpace(serviceTier)
	}
	if model := effectiveCodexModel(a, sess, ws); strings.TrimSpace(model) != "" {
		params["model"] = strings.TrimSpace(model)
	}

	var result codexrpc.ThreadStartResult
	ctx, cancel := context.WithTimeout(a.Context(), 20*time.Second)
	defer cancel()
	if err := client.Call(ctx, "thread/fork", params, &result); err != nil {
		return "", err
	}
	forkedID := strings.TrimSpace(result.Thread.ID)
	if forkedID == "" {
		return "", fmt.Errorf("fork thread returned empty thread id")
	}
	if err := persistForkedConversation(a, sessionKey, sess, workspaceID, forkedID, result.Thread.Name, result.Thread.Preview, false); err != nil {
		return "", err
	}
	return forkedID, nil
}

func persistForkedConversation(a *App, sessionKey string, sess *conversation.Session, workspaceID, threadID, name, preview string, resetThreadSettings bool) error {
	if sess == nil {
		return fmt.Errorf("session not found")
	}
	if resetThreadSettings {
		conversation.ClearThreadContext(sess)
	}
	conversation.SetThreadContext(sess, workspaceID, threadID, name, preview)
	sess.ActiveThreadCollaborationMode = nil
	if strings.TrimSpace(threadID) != "" {
		markSessionThreadLive(a, sessionKey, threadID)
	} else {
		clearSessionLiveThread(a, sessionKey)
	}
	conversation.ResetActiveOperations(sess)
	sess.Status = conversation.SessionStatusIdle.String()
	sess.Queue = nil
	sess.StagedImages = nil
	return a.State().SaveSession(sess)
}
