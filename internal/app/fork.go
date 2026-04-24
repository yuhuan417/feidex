package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (s conversationWorkflowService) commandFork(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /fork")
	}
	if msg == nil {
		return nil
	}
	discarded, forkedID, err := s.app.startThreadFork(s.app.makeSessionKey(msg))
	if err != nil {
		return err
	}
	reply := s.app.conversationBackend().forkReplyMessage(forkedID)
	if discarded > 0 {
		reply += fmt.Sprintf(" 已丢弃 %d 条排队或暂存输入。", discarded)
	}
	return s.app.feishu.ReplyText(context.Background(), msg.MessageID, reply, s.app.replyInThreadEnabled(msg.ChatType))
}

func (a *App) startThreadFork(sessionKey string) (int, string, error) {
	if a == nil || a.store == nil {
		return 0, "", fmt.Errorf("store not initialized")
	}
	appState := a.appState()
	sess := appState.session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return 0, "", fmt.Errorf("%s，无法 fork", primaryConversationMissingLabel(a.configuredBackend()))
	}
	if sessionHasActiveWork(sess) {
		return 0, "", fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), a.defaultWorkspaceID())
	ws := config.FindWorkspace(a.cfg, workspaceID)
	if ws == nil {
		return 0, "", fmt.Errorf("workspace %q not found", workspaceID)
	}
	discarded := newPendingQueueService(a).discardSessionPendingInputs(sessionKey)
	sess = appState.session(sessionKey)
	if sess == nil {
		return 0, "", fmt.Errorf("session %q disappeared", sessionKey)
	}
	forkedID, err := a.conversationBackend().forkActiveConversation(sessionKey, sess, ws)
	if err != nil {
		return 0, "", err
	}
	return discarded, forkedID, nil
}

func (s conversationWorkflowService) completeMenuFork(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.app.completeMenuCommand(action, sessionKey, primaryConversationSlash(s.app.configuredBackend())+" fork", "menu.thread")
}
