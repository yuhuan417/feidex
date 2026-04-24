package app

import (
	"context"
	"strings"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type backendActionFacade interface {
	runMenuCompactAction(a *App, action *feishu.CardAction, sessionKey string) error
	handleCompactCommand(a *App, msg *feishu.InboundMessage) error
	completeMenuInterrupt(a *App, action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error)
}

func backendActionForKind(kind string) backendActionFacade {
	switch normalizeRuntimeBackend(kind) {
	case backendCodex:
		return codexBackendActions{}
	case backendClaude:
		return claudeBackendActions{}
	default:
		return nil
	}
}

func (a *App) backendActions() backendActionFacade {
	if a == nil {
		return nil
	}
	return backendActionForKind(a.configuredBackend())
}

type codexBackendActions struct{}

func (codexBackendActions) runMenuCompactAction(a *App, action *feishu.CardAction, sessionKey string) error {
	if a == nil {
		return nil
	}
	msg := a.commandMessageFromAction(action, sessionKey, "/compact")
	sessionKey = firstNonEmpty(a.makeSessionKey(msg), strings.TrimSpace(sessionKey))
	_, err := a.startThreadCompaction(sessionKey)
	return err
}

func (codexBackendActions) handleCompactCommand(a *App, msg *feishu.InboundMessage) error {
	if a == nil || msg == nil {
		return nil
	}
	if _, err := a.startThreadCompaction(a.makeSessionKey(msg)); err != nil {
		return err
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "已请求压缩当前线程上下文。", a.replyInThreadEnabled(msg.ChatType))
}

func (codexBackendActions) completeMenuInterrupt(a *App, action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	parentAction := actionStringValue(action, "parent_action")
	return a.completeMenuCommand(action, sessionKey, "/stop", parentAction)
}

type claudeBackendActions struct{}

func (claudeBackendActions) runMenuCompactAction(a *App, action *feishu.CardAction, sessionKey string) error {
	if a == nil {
		return nil
	}
	msg := a.commandMessageFromAction(action, sessionKey, "/compact")
	return a.enqueueSubmission(msg)
}

func (claudeBackendActions) handleCompactCommand(a *App, msg *feishu.InboundMessage) error {
	if a == nil || msg == nil {
		return nil
	}
	return newCommandService(a).enqueuePassthroughCommand(msg, "/compact")
}

func (claudeBackendActions) completeMenuInterrupt(a *App, action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	parentAction := actionStringValue(action, "parent_action")
	return a.completeAsyncCommandAction(
		action,
		sessionKey,
		"/stop",
		parentAction,
		"正在请求中断当前任务",
		a.renderInterruptPreparingCard(sessionKey, parentAction),
		func(sessionKey, text string) map[string]any {
			return a.renderInterruptResultCard(sessionKey, parentAction, text)
		},
		func(sessionKey, errText string) map[string]any {
			return a.renderInterruptFailedCard(sessionKey, parentAction, targetTurnID, errText)
		},
		"interrupt patch failed",
	)
}
