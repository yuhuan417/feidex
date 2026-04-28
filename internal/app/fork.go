package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func commandFork(a *App, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /fork")
	}
	if msg == nil {
		return nil
	}
	discarded, forkedID, err := startThreadFork(a, makeSessionKey(a, msg))
	if err != nil {
		return err
	}
	reply := conversationBackend(a).forkReplyMessage(forkedID)
	if discarded > 0 {
		reply += fmt.Sprintf(" 已丢弃 %d 条排队或暂存输入。", discarded)
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, replyInThreadEnabled(a, msg.ChatType))
}

func startThreadFork(a *App, sessionKey string) (int, string, error) {
	if a == nil || a.store == nil {
		return 0, "", fmt.Errorf("store not initialized")
	}
	appState := a.State()
	sess := appState.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return 0, "", fmt.Errorf("%s，无法 fork", primaryConversationMissingLabel(configuredBackend(a)))
	}
	if sessionHasActiveWork(sess) {
		return 0, "", fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), defaultWorkspaceID(a))
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return 0, "", fmt.Errorf("workspace %q not found", workspaceID)
	}
	discarded := newPendingQueueService(a).discardSessionPendingInputs(sessionKey)
	sess = appState.Session(sessionKey)
	if sess == nil {
		return 0, "", fmt.Errorf("session %q disappeared", sessionKey)
	}
	forkedID, err := conversationBackend(a).forkActiveConversation(sessionKey, sess, ws)
	if err != nil {
		return 0, "", err
	}
	return discarded, forkedID, nil
}

func completeMenuFork(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(a, action, sessionKey, primaryConversationSlash(configuredBackend(a))+" fork", "menu.thread")
}
