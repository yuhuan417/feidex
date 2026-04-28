package backend

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/app/appcore"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type CompactRunner interface {
	StartThreadCompaction(sessionKey string) (*state.Session, error)
}

type ActionCommandDeps struct {
	CommandMessageFromAction   func(action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage
	CompleteMenuCommand        func(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error)
	CompleteAsyncCommandAction func(
		action *feishu.CardAction,
		sessionKey, rawCommand, fallbackAction, toastText string,
		preparingCard map[string]any,
		successCardFromText func(sessionKey, text string) map[string]any,
		failureCard func(sessionKey, errText string) map[string]any,
		patchWarnMsg string,
	) (*callback.CardActionTriggerResponse, error)
}

type ActionRenderDeps struct {
	RenderInterruptPreparingCard func(sessionKey, parentAction string) map[string]any
	RenderInterruptResultCard    func(sessionKey, parentAction, text string) map[string]any
	RenderInterruptFailedCard    func(sessionKey, parentAction, targetTurnID, errText string) map[string]any
}

type ActionExecutionDeps struct {
	EnqueueSubmission         func(msg *feishu.InboundMessage) error
	EnqueuePassthroughCommand func(msg *feishu.InboundMessage, rawCommand string) error
	ReplyText                 func(ctx context.Context, msgID, text string, inThread bool) error
	ReplyInThreadEnabled      func(chatType string) bool
}

type ActionDeps struct {
	App       App
	Commands  ActionCommandDeps
	Render    ActionRenderDeps
	Execution ActionExecutionDeps
}

type ActionService struct {
	App  App
	deps ActionDeps
}

func NewActionService(deps ActionDeps) ActionService {
	return ActionService{App: deps.App, deps: deps}
}

func (s ActionService) RunMenuCompactAction(action *feishu.CardAction, sessionKey string, runner CompactRunner) error {
	if s.App == nil {
		return nil
	}
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendClaude:
		if s.deps.Commands.CommandMessageFromAction == nil || s.deps.Execution.EnqueueSubmission == nil {
			return fmt.Errorf("backend compact action not configured")
		}
		msg := s.deps.Commands.CommandMessageFromAction(action, sessionKey, "/compact")
		return s.deps.Execution.EnqueueSubmission(msg)
	case appruntime.BackendCodex:
		if runner == nil {
			return fmt.Errorf("compact runner not configured")
		}
		_, err := runner.StartThreadCompaction(sessionKey)
		return err
	default:
		return unsupportedBackendError(appcore.ConfiguredBackend(s.App))
	}
}

func (s ActionService) HandleCompactCommand(msg *feishu.InboundMessage, runner CompactRunner) error {
	if s.App == nil || msg == nil {
		return nil
	}
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendClaude:
		if s.deps.Execution.EnqueuePassthroughCommand == nil {
			return fmt.Errorf("passthrough command handler not configured")
		}
		return s.deps.Execution.EnqueuePassthroughCommand(msg, "/compact")
	case appruntime.BackendCodex:
		if runner == nil {
			return fmt.Errorf("compact runner not configured")
		}
		sessionKey := strings.TrimSpace(appcore.MakeSessionKey(s.App, msg))
		if _, err := runner.StartThreadCompaction(sessionKey); err != nil {
			return err
		}
		if s.deps.Execution.ReplyText == nil || s.deps.Execution.ReplyInThreadEnabled == nil {
			return nil
		}
		return s.deps.Execution.ReplyText(context.Background(), msg.MessageID, "已请求压缩当前线程上下文。", s.deps.Execution.ReplyInThreadEnabled(msg.ChatType))
	default:
		return unsupportedBackendError(appcore.ConfiguredBackend(s.App))
	}
}

func (s ActionService) CompleteMenuInterrupt(action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	parentAction := actionStringValue(action, "parent_action")
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendClaude:
		if s.deps.Commands.CompleteAsyncCommandAction == nil || s.deps.Render.RenderInterruptPreparingCard == nil || s.deps.Render.RenderInterruptResultCard == nil || s.deps.Render.RenderInterruptFailedCard == nil {
			return nil, fmt.Errorf("interrupt action not configured")
		}
		return s.deps.Commands.CompleteAsyncCommandAction(
			action,
			sessionKey,
			"/stop",
			parentAction,
			"正在请求中断当前任务",
			s.deps.Render.RenderInterruptPreparingCard(sessionKey, parentAction),
			func(sessionKey, text string) map[string]any {
				return s.deps.Render.RenderInterruptResultCard(sessionKey, parentAction, text)
			},
			func(sessionKey, errText string) map[string]any {
				return s.deps.Render.RenderInterruptFailedCard(sessionKey, parentAction, targetTurnID, errText)
			},
			"interrupt patch failed",
		)
	case appruntime.BackendCodex:
		if s.deps.Commands.CompleteMenuCommand == nil {
			return nil, fmt.Errorf("interrupt action not configured")
		}
		return s.deps.Commands.CompleteMenuCommand(action, sessionKey, "/stop", parentAction)
	default:
		return unsupportedBackendActionResponse(appcore.ConfiguredBackend(s.App)), nil
	}
}

func actionStringValue(action *feishu.CardAction, key string) string {
	if action == nil {
		return ""
	}
	value, _ := action.ActionValue[key].(string)
	return strings.TrimSpace(value)
}
