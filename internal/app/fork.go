package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (a *App) commandFork(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /fork")
	}
	if msg == nil {
		return nil
	}
	discarded, err := a.startThreadFork(a.makeSessionKey(msg))
	if err != nil {
		return err
	}
	reply := "已 fork 当前线程，并切换到新的分支线程。"
	if discarded > 0 {
		reply += fmt.Sprintf(" 已丢弃 %d 条排队或暂存输入。", discarded)
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, a.replyInThreadEnabled(msg.ChatType))
}

func (a *App) startThreadFork(sessionKey string) (int, error) {
	if a == nil || a.store == nil {
		return 0, fmt.Errorf("store not initialized")
	}
	appState := a.appState()
	sess := appState.session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return 0, fmt.Errorf("当前没有活动线程，无法 fork")
	}
	if sessionHasActiveWork(sess) {
		return 0, fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), a.defaultWorkspaceID())
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return 0, fmt.Errorf("workspace %q not found", workspaceID)
	}
	discarded := a.discardSessionPendingInputs(sessionKey)
	sess = appState.session(sessionKey)
	if sess == nil {
		return 0, fmt.Errorf("session %q disappeared", sessionKey)
	}

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
	if err := a.codex.Call(ctx, "thread/fork", params, &result); err != nil {
		return 0, err
	}
	newThreadID := strings.TrimSpace(result.Thread.ID)
	if newThreadID == "" {
		return 0, fmt.Errorf("fork thread returned empty thread id")
	}
	setSessionThreadContext(sess, workspaceID, newThreadID, result.Thread.Name, result.Thread.Preview)
	a.markSessionThreadLive(sessionKey, newThreadID)
	sessionResetActiveOperations(sess)
	sess.Status = "idle"
	sess.Queue = nil
	sess.StagedImages = nil
	if err := appState.saveSession(sess); err != nil {
		return 0, err
	}
	return discarded, nil
}

func (a *App) completeMenuFork(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return a.completeMenuCommand(action, sessionKey, "/thread fork", "menu.thread")
}
