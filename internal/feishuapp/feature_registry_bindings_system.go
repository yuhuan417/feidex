package feishuapp

import (
	"feidex/internal/adapter/feishu/upgraderender"

	"context"
	"time"

	"feidex/internal/feishu"
)

func appendFeatureBindingsSystem(bindings map[string]featureBinding) {
	bindings["menu.debug"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"debug": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.Debug.CommandDebug(msg, args)
				},
			},
		},
		RenderActions: []string{"menu.debug.logs"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.debug.logs" {
				return nil, false
			}
			return a.bindings.Debug.RenderDebugLogsCard(sessionKey), true
		},
		PortActions: []string{"menu.debug", "menu.debug.logs"},
	}
	bindings["menu.status"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"status": {
				Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
					return commandStatus(a, msg)
				},
			},
		},
		PortActions: []string{"menu.status"},
	}
	bindings["menu.help"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"help": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return commandHelp(a, msg, args)
				},
			},
		},
		PortActions: []string{"menu.help"},
	}
	bindings["menu.codex_upgrade"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"codex": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.BackendUpgrades.commandCodex(msg, args)
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
			view, err := a.bindings.BackendUpgrades.loadCodexUpgradeView(ctx, false)
			if err != nil {
				return nil, false
			}
			return a.bindings.UpgradePresentation.renderUpgradeStatusCard(upgraderender.CodexSpec, sessionKey, view, false), true
		},
		PortActions: []string{"menu.codex_upgrade"},
	}
	bindings["menu.claude_upgrade"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"claude": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.BackendUpgrades.commandClaude(msg, args)
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
			view, err := a.bindings.BackendUpgrades.loadClaudeUpgradeView(ctx, false)
			if err != nil {
				return nil, false
			}
			return a.bindings.UpgradePresentation.renderUpgradeStatusCard(upgraderender.ClaudeSpec, sessionKey, view, false), true
		},
		PortActions: []string{"menu.claude_upgrade"},
	}
	bindings["menu.upgrade"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"upgrade": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.Upgrades.CommandUpgrade(msg, args)
				},
			},
		},
		PortActions: []string{"menu.upgrade"},
	}
}
