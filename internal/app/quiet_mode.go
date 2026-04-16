package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

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

func (a *App) quietMode() config.QuietMode {
	if a == nil || a.cfg == nil {
		return config.QuietModeVerbose
	}
	mode, err := config.ParseQuietMode(a.cfg.Feishu.Quiet)
	if err != nil {
		return config.QuietModeVerbose
	}
	return mode
}

func (a *App) quietModeEnabled() bool {
	return a.quietMode() != config.QuietModeVerbose
}

func (a *App) quietWorkingCardEnabled() bool {
	return a.quietMode() == config.QuietModeProgress
}

func quietModeStatusText(mode config.QuietMode) string {
	return mode.String()
}

func quietModeDescription(mode config.QuietMode) string {
	for _, option := range quietModeOptions {
		if option.Mode == mode {
			return option.Description
		}
	}
	return ""
}

func shouldDeliverTurnKindInQuiet(mode config.QuietMode, kind string) bool {
	switch mode {
	case config.QuietModeProgress:
		switch strings.TrimSpace(kind) {
		case "final_message", "turn_output", "turn_plan", "turn_queued", "turn_terminal":
			return true
		default:
			return false
		}
	case config.QuietModeNormal:
		switch strings.TrimSpace(kind) {
		case "final_message", "turn_output", "turn_plan":
			return true
		default:
			return false
		}
	case config.QuietModeFinal:
		return strings.TrimSpace(kind) == "final_message"
	default:
		return true
	}
}

func shouldDeliverTurnItemInQuiet(mode config.QuietMode, itemType string, isFinalAnswer bool) bool {
	switch mode {
	case config.QuietModeProgress, config.QuietModeNormal:
		switch normalizeTurnItemType(itemType) {
		case "plan", "agent_message", "exited_review_mode":
			return true
		default:
			return false
		}
	case config.QuietModeFinal:
		switch normalizeTurnItemType(itemType) {
		case "agent_message", "exited_review_mode":
			return isFinalAnswer
		default:
			return false
		}
	default:
		return true
	}
}

func (a *App) renderQuietModeCard() map[string]any {
	return a.renderQuietModeMenuCard("")
}

func (a *App) renderQuietModeMenuCard(sessionKey string) map[string]any {
	mode := a.quietMode()
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
	normalized, err := config.ParseQuietMode(mode)
	if err != nil {
		return err
	}
	a.cfg.Feishu.Quiet = normalized
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
		card := a.renderQuietModeMenuCard(a.makeSessionKey(msg))
		_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
		return err
	}
	arg := strings.TrimSpace(args[0])
	if len(args) == 1 {
		switch arg {
		case "config":
			if msg == nil {
				return nil
			}
			card := a.renderQuietModeMenuCard(a.makeSessionKey(msg))
			_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
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
			return a.feishu.ReplyText(context.Background(), msg.MessageID, "Quiet Mode 已切换为 `"+quietModeStatusText(mode)+"`。", msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
		}
	}
	return nil
}
