package feishuapp

import (
	"context"
	"feidex/internal/application"
	conversationapp "feidex/internal/application/conversation"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/runtime"
	"feidex/internal/textutil"
	"fmt"
	"strings"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type threadForkDependencies struct {
	repository interface {
		Session(string) *conversation.Session
	}
	config              *config.Config
	effectiveSessionKey func(string) string
	makeSessionKey      func(*feishu.InboundMessage) string
	backend             func() string
	defaultWorkspaceID  func() string
	pendingQueue        interface{ DiscardSessionPendingInputs(string) int }
	conversations       *conversationapp.Service
	runner              runtime.EffectRunner
	frontend            identity.FrontendID
	replyInThread       bool
}

func commandFork(deps threadForkDependencies, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /fork")
	}
	if msg == nil {
		return nil
	}
	sessionKey := deps.effectiveSessionKey(deps.makeSessionKey(msg))
	discarded, forkedID, err := startThreadFork(deps, sessionKey)
	if err != nil {
		return err
	}
	reply := forkReplyMessage(deps.backend(), forkedID)
	if discarded > 0 {
		reply += fmt.Sprintf(" 已丢弃 %d 条排队或暂存输入。", discarded)
	}
	return deps.runner.Run(context.Background(), []application.Effect{application.SendMessage{
		Frontend:       deps.frontend,
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		Text:           reply,
		InThread:       deps.replyInThread,
	}})
}

func startThreadFork(deps threadForkDependencies, sessionKey string) (int, string, error) {
	if deps.repository == nil {
		return 0, "", fmt.Errorf("store not initialized")
	}
	sess := deps.repository.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return 0, "", fmt.Errorf("%s，无法 fork", primaryConversationMissingLabel(deps.backend()))
	}
	if sessionHasActiveWork(sess) {
		return 0, "", fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	workspaceID := textutil.FirstNonEmpty(strings.TrimSpace(sess.WorkspaceID), deps.defaultWorkspaceID())
	ws := config.FindWorkspace(deps.config, workspaceID)
	if ws == nil {
		return 0, "", fmt.Errorf("workspace %q not found", workspaceID)
	}
	discarded := deps.pendingQueue.DiscardSessionPendingInputs(sessionKey)
	sess = deps.repository.Session(sessionKey)
	if sess == nil {
		return 0, "", fmt.Errorf("session %q disappeared", sessionKey)
	}
	forkedID, err := deps.conversations.ForkActiveConversation(sessionKey, sess, ws)
	if err != nil {
		return 0, "", err
	}
	return discarded, forkedID, nil
}

func completeMenuFork(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	sessionKey = threadMenuEffectiveSessionKey(a.configView().normalizeSessionKey, a.bindings.BindingCommands.scope, a.bindings.ConversationQuery, sessionKey)
	return completeMenuCommand(a, action, sessionKey, primaryConversationSlash(a.configView().configuredBackend())+" fork", "menu.thread")
}
