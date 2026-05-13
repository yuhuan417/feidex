package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"feidex/internal/app/quietmode"
	"feidex/internal/config"
	"feidex/internal/feishu"
)

type quietModeOption = quietmode.Option

var quietModeOptions = quietmode.Options

var quietMode = quietmode.Mode

var quietModeEnabled = quietmode.Enabled

var quietWorkingCardEnabled = quietmode.WorkingCardEnabled

var quietModeStatusText = quietmode.StatusText

var shouldDeliverTurnKindInQuiet = quietmode.ShouldDeliverTurnKind

func shouldDeliverTurnItemInQuiet(mode config.QuietMode, itemType string, isFinalAnswer bool) bool {
	return quietmode.ShouldDeliverTurnItem(mode, itemType, isFinalAnswer)
}

func shouldDeliverTurnItemPayloadInQuiet(mode config.QuietMode, payload turnItemCardPayload) bool {
	return quietmode.ShouldDeliverTurnItemPayload(mode, payload.ItemType, payload.ProtocolItemType, payload.ToolName, payload.IsFinalAnswer)
}

func isClaudeTodoToolPayload(payload turnItemCardPayload) bool {
	return quietmode.IsClaudeTodoToolPayload(payload.ProtocolItemType, payload.ToolName)
}

func renderQuietModeCard(a *App) map[string]any {
	return renderQuietModeMenuCard(a, "")
}

func renderQuietModeMenuCard(a *App, sessionKey string) map[string]any {
	mode := quietMode(feishuConfig(a))
	lines := []string{
		"当前模式: `" + quietModeStatusText(mode) + "`",
		"",
	}
	for _, option := range quietModeOptions {
		lines = append(lines, "- `"+option.Title+"`: "+option.Description)
	}
	buttons := make([]feishu.Button, 0, len(quietModeOptions)+1)
	for _, option := range quietModeOptions {
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
			Text:  "返回上一级",
			Type:  "default",
			Value: map[string]any{"action": "menu.tools", "session_key": sessionKey},
		})
	}
	return a.feishu.SimpleStatusCard(planModeTitleForSession(a, sessionKey, "Quiet Mode"), "blue", menuCardBodyForSession(a, sessionKey, "menu.quiet", strings.Join(lines, "\n")), buttons)
}

func updateQuietMode(a *App, mode config.QuietMode) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("nil config")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	cfg := feishuConfigUnlocked(a)
	if cfg == nil {
		return fmt.Errorf("nil feishu config")
	}
	normalized, err := config.ParseQuietMode(mode)
	if err != nil {
		return err
	}
	cfg.Quiet = normalized
	if err := a.cfg.Normalize(filepath.Dir(a.cfgPath)); err != nil {
		return err
	}
	return config.Save(a.cfgPath, a.cfg)
}

func commandQuiet(a *App, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: /quiet | /quiet <verbose|progress|normal|final> | /quiet config")
	}
	if len(args) == 0 {
		if msg == nil {
			return nil
		}
		card := renderQuietModeMenuCard(a, makeSessionKey(a, msg))
		_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(a, msg.ChatType))
		return err
	}
	arg := strings.TrimSpace(args[0])
	if len(args) == 1 {
		switch arg {
		case "config":
			if msg == nil {
				return nil
			}
			card := renderQuietModeMenuCard(a, makeSessionKey(a, msg))
			_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(a, msg.ChatType))
			return err
		default:
			mode, err := config.ParseQuietMode(config.QuietMode(arg))
			if err != nil {
				return fmt.Errorf("usage: /quiet | /quiet <verbose|progress|normal|final> | /quiet config")
			}
			if msg == nil {
				return nil
			}
			if err := updateQuietMode(a, mode); err != nil {
				return err
			}
			return a.feishu.ReplyText(context.Background(), msg.MessageID, "Quiet Mode 已切换为 `"+quietModeStatusText(mode)+"`。", replyInThreadEnabled(a, msg.ChatType))
		}
	}
	return nil
}
