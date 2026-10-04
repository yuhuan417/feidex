package feishuapp

import (
	"fmt"
	"strings"

	"feidex/internal/adapter/feishu/quietmode"
	"feidex/internal/adapter/feishu/turnitem"
	"feidex/internal/application/runtimeconfig"
	"feidex/internal/config"
	"feidex/internal/feishu"
)

func shouldDeliverTurnItemInQuiet(mode config.QuietMode, itemType string, isFinalAnswer bool) bool {
	return quietmode.ShouldDeliverTurnItem(mode, itemType, isFinalAnswer)
}

func shouldDeliverTurnItemPayloadInQuiet(mode config.QuietMode, payload turnitem.CardPayload) bool {
	return quietmode.ShouldDeliverTurnItemPayload(mode, payload.ItemType, payload.ProtocolItemType, payload.ToolName, payload.IsFinalAnswer)
}

func renderQuietModeMenuCard(mode config.QuietMode, sessionKey, title string, renderer bindingCardRenderer) map[string]any {
	lines := []string{
		"当前模式: `" + quietmode.StatusText(mode) + "`",
		"",
	}
	for _, option := range quietmode.Options {
		lines = append(lines, "- `"+option.Title+"`: "+option.Description)
	}
	buttons := make([]feishu.Button, 0, len(quietmode.Options)+1)
	for _, option := range quietmode.Options {
		buttons = append(buttons, feishu.Button{
			Text: func() string {
				if option.Mode == mode {
					return "当前 · " + option.Title
				}
				return option.Title
			}(),
			Type: func() string {
				if option.Mode == mode {
					return "primary"
				}
				return "default"
			}(),
			Value: map[string]any{
				"action":      "quiet.set",
				"mode":        option.Mode.String(),
				"session_key": sessionKey,
			},
		})
	}
	if strings.TrimSpace(sessionKey) != "" {
		buttons = append(buttons, feishu.Button{
			Text:  feishu.MenuBackButtonText,
			Type:  "default",
			Value: map[string]any{"action": "menu.tools", "session_key": sessionKey},
		})
	}
	return renderer.SimpleStatusCard(title, "blue", menuCardBody("menu.quiet", strings.Join(lines, "\n")), buttons)
}

func updateQuietMode(settings runtimeconfig.Service, mode config.QuietMode) error {
	if settings.Repository == nil {
		return fmt.Errorf("nil config")
	}
	normalized, err := config.ParseQuietMode(mode)
	if err != nil {
		return err
	}
	return settings.SetQuietMode(normalized.String())
}

func commandQuiet(a *App, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: /quiet | /quiet <verbose|progress|normal|final> | /quiet config")
	}
	if len(args) == 0 {
		if msg == nil {
			return nil
		}
		sessionKey := a.configView().makeSessionKey(msg)
		card := renderQuietModeMenuCard(quietmode.Mode(a.configView().feishuConfig()), sessionKey, planModeTitleForSession(a, sessionKey, "Quiet Mode"), a.feishu)
		return replyCardEffect(a, msg, card)
	}
	arg := strings.TrimSpace(args[0])
	if len(args) == 1 {
		switch arg {
		case "config":
			if msg == nil {
				return nil
			}
			sessionKey := a.configView().makeSessionKey(msg)
			card := renderQuietModeMenuCard(quietmode.Mode(a.configView().feishuConfig()), sessionKey, planModeTitleForSession(a, sessionKey, "Quiet Mode"), a.feishu)
			return replyCardEffect(a, msg, card)
		default:
			mode, err := config.ParseQuietMode(config.QuietMode(arg))
			if err != nil {
				return fmt.Errorf("usage: /quiet | /quiet <verbose|progress|normal|final> | /quiet config")
			}
			if msg == nil {
				return nil
			}
			if err := updateQuietMode(a.bindings.RuntimeSettings, mode); err != nil {
				return err
			}
			return replyTextEffect(a, msg, "Quiet Mode 已切换为 `"+quietmode.StatusText(mode)+"`。")
		}
	}
	return nil
}
