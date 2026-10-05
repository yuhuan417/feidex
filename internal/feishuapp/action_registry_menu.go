package feishuapp

import (
	retryview "feidex/internal/adapter/feishu/autoretry"
	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/feishu"
	"strings"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type MenuCoreCardActionInputs struct {
	Backend             func() string
	State               planmode.SessionStateProvider
	Renderer            bindingCardRenderer
	BackendSelection    appbackend.SelectionService
	AutoRetry           retryview.Service
	CompleteMenuCommand func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
}

type BindingCardActionInputs struct {
	Backend             func() string
	State               planmode.SessionStateProvider
	Renderer            bindingCardRenderer
	BindingCommands     bindingService
	CompleteMenuCommand func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
}

func bindingCardActionHandlers(inputs BindingCardActionInputs) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{
		"menu.current_bot": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "info", Content: "已返回命令菜单"},
				Card:  rawCard(renderCommandMenuCardData(inputs.Backend(), planModeTitleForSession(inputs.State, true, sessionKey, "主菜单"), inputs.Renderer, sessionKey)),
			}, nil
		},
		"menu.current_workspace": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/workspace", "menu.root")
		},
		"current_workspace.choose": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/workspace choose", "menu.workspace")
		},
		"current_workspace.use": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.BindingCommands.completeBindingUse(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
		},
	}
}

func menuCoreCardActionHandlers(inputs MenuCoreCardActionInputs) map[string]cardActionPortHandler {
	menuCard := func(action *feishu.CardAction, toastText, title string, render func(string, string, bindingCardRenderer, string) map[string]any) (*callback.CardActionTriggerResponse, error) {
		sessionKey := actionSessionKey(action)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: toastText},
			Card:  rawCard(render(inputs.Backend(), planModeTitleForSession(inputs.State, true, sessionKey, title), inputs.Renderer, sessionKey)),
		}, nil
	}
	return map[string]cardActionPortHandler{
		"menu.root": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return menuCard(action, "已返回命令菜单", "主菜单", renderCommandMenuCardData)
		},
		"menu.tools": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			spec, _ := menuGroupSpec("menu.tools")
			return menuCard(action, "已打开常用工具", spec.Label, renderToolsMenuCardData)
		},
		"menu.group.model": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/model", "menu.group.model")
		},
		"menu.group.system": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			spec, _ := menuGroupSpec("menu.group.system")
			return menuCard(action, "已打开 system", spec.Label, renderSystemMenuCardData)
		},
		"menu.group.backend": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.BackendSelection.CompleteMenuBackend(action, actionSessionKey(action))
		},
		"menu.backend": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "已打开切换后端"}, Card: rawCard(inputs.BackendSelection.RenderBackendSelectionCard(actionSessionKey(action), ""))}, nil
		},
		"menu.backend.switch": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "已打开切换后端"}, Card: rawCard(inputs.BackendSelection.RenderBackendSelectionCard(actionSessionKey(action), ""))}, nil
		},
		"menu.auto_retry": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/backend retry", "menu.group.backend")
		},
		"backend.select": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.BackendSelection.CompleteBackendSelect(action, actionSessionKey(action), actionStringValue(action, "backend"))
		},
		"auto_retry.set": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.AutoRetry.CompleteAutoRetrySet(action, strings.EqualFold(actionStringValue(action, "enabled"), "on"))
		},
	}
}
