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

type quietModeOption struct {
	Mode        config.QuietMode
	Title       string
	Description string
}

var quietModeOptions = []quietModeOption{
	{
		Mode:        config.QuietModeVerbose,
		Title:       "verbose",
		Description: "完整展开所有过程消息。",
	},
	{
		Mode:        config.QuietModeProgress,
		Title:       "progress",
		Description: "把两次 plan / agent message 之间的过程折叠成一张持续更新的 `工作中` 卡。",
	},
	{
		Mode:        config.QuietModeNormal,
		Title:       "normal",
		Description: "只发送 plan 和 agent / final message，不显示 `工作中` 卡。",
	},
	{
		Mode:        config.QuietModeFinal,
		Title:       "final",
		Description: "只保留最终答复。",
	},
}

func quietMode(cfg *config.FeishuConfig) config.QuietMode {
	if cfg == nil {
		return config.QuietModeProgress
	}
	mode, err := config.ParseQuietMode(cfg.Quiet)
	if err != nil {
		return config.QuietModeProgress
	}
	return mode
}

func quietModeEnabled(cfg *config.FeishuConfig) bool {
	return quietMode(cfg) != config.QuietModeVerbose
}

func quietWorkingCardEnabled(cfg *config.FeishuConfig) bool {
	return quietMode(cfg) == config.QuietModeProgress
}

func quietModeStatusText(mode config.QuietMode) string {
	return quietmode.StatusText(mode)
}

func shouldDeliverTurnKindInQuiet(mode config.QuietMode, kind string) bool {
	return quietmode.ShouldDeliverTurnKind(mode, kind)
}

func shouldDeliverTurnItemInQuiet(mode config.QuietMode, itemType string, isFinalAnswer bool) bool {
	return quietmode.ShouldDeliverTurnItem(mode, itemType, isFinalAnswer)
}

func shouldDeliverTurnItemPayloadInQuiet(mode config.QuietMode, payload turnItemCardPayload) bool {
	return quietmode.ShouldDeliverTurnItemPayload(mode, payload.ItemType, payload.ProtocolItemType, payload.ToolName, payload.IsFinalAnswer)
}

func isClaudeTodoToolPayload(payload turnItemCardPayload) bool {
	return quietmode.IsClaudeTodoToolPayload(payload.ProtocolItemType, payload.ToolName)
}

func (a *App) renderQuietModeCard() map[string]any {
	return a.renderQuietModeMenuCard("")
}

func (a *App) renderQuietModeMenuCard(sessionKey string) map[string]any {
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
	return a.feishu.SimpleStatusCard("Quiet Mode", "blue", menuCardBody("menu.quiet", strings.Join(lines, "\n")), buttons)
}

func (a *App) updateQuietMode(mode config.QuietMode) error {
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

func (a *App) commandQuiet(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: /quiet | /quiet <verbose|progress|normal|final> | /quiet config")
	}
	if len(args) == 0 {
		if msg == nil {
			return nil
		}
		card := a.renderQuietModeMenuCard(makeSessionKey(a, msg))
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
			card := a.renderQuietModeMenuCard(makeSessionKey(a, msg))
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
			if err := a.updateQuietMode(mode); err != nil {
				return err
			}
			return a.feishu.ReplyText(context.Background(), msg.MessageID, "Quiet Mode 已切换为 `"+quietModeStatusText(mode)+"`。", replyInThreadEnabled(a, msg.ChatType))
		}
	}
	return nil
}
