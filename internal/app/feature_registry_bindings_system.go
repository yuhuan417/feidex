package app

import (
	"context"
	"time"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func appendFeatureBindingsSystem(bindings map[string]featureBinding) {
	bindings["menu.debug"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"debug": {
				Match: func(fields []string) bool {
					return exactOrSingleArgCommand(fields, "on", "off", "logs")
				},
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return newDebugServiceInner(a).CommandDebug(msg, args)
				},
			},
		},
		RenderActions: []string{"menu.debug.logs"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.debug.logs" {
				return nil, false
			}
			return newDebugServiceInner(a).RenderDebugLogsCard(sessionKey), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			switch actionName {
			case "menu.debug":
				return newDebugServiceInner(s.app).CompleteMenuDebug(action, sessionKey)
			case "menu.debug.logs":
				return newDebugServiceInner(s.app).CompleteMenuDebugLogs(action, sessionKey)
			default:
				return nil, nil
			}
		},
	}
	bindings["menu.status"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"status": {
				Match: exactCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
					return commandStatus(a, msg)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.status" {
				return nil, nil
			}
			return newMenuActionService(s.app).completeMenuStatus(action, actionSessionKey(action))
		},
	}
	bindings["menu.help"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"help": {
				Match: exactCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return commandHelp(a, msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.help" {
				return nil, nil
			}
			return newMenuActionService(s.app).completeMenuHelp(action, actionSessionKey(action))
		},
	}
	bindings["menu.codex_upgrade"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"codex": {
				Match: matchCodexCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return newBackendUpgradeService(a).commandCodex(msg, args)
				},
			},
		},
		RenderActions: []string{"menu.codex_upgrade"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.codex_upgrade" {
				return nil, false
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			view, err := newBackendUpgradeService(a).loadCodexUpgradeView(ctx, false)
			if err != nil {
				return nil, false
			}
			return newUpgradeRenderService(a).renderCodexUpgradeStatusCard(sessionKey, view, false), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.codex_upgrade" {
				return nil, nil
			}
			return newBackendUpgradeService(s.app).completeMenuCodexUpgrade(action)
		},
	}
	bindings["menu.claude_upgrade"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"claude": {
				Match: matchClaudeCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return newBackendUpgradeService(a).commandClaude(msg, args)
				},
			},
		},
		RenderActions: []string{"menu.claude_upgrade"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.claude_upgrade" {
				return nil, false
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			view, err := newBackendUpgradeService(a).loadClaudeUpgradeView(ctx, false)
			if err != nil {
				return nil, false
			}
			return newUpgradeRenderService(a).renderClaudeUpgradeStatusCard(sessionKey, view, false), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.claude_upgrade" {
				return nil, nil
			}
			return newBackendUpgradeService(s.app).completeMenuClaudeUpgrade(action)
		},
	}
	bindings["menu.upgrade"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"upgrade": {
				Match: matchUpgradeCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return newUpgradeServiceInner(a).CommandUpgrade(msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.upgrade" {
				return nil, nil
			}
			return newMenuActionService(s.app).completeMenuUpgrade(action)
		},
	}
}
