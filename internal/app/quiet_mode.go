package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
)

func (a *App) quietModeEnabled() bool {
	return a != nil && a.cfg != nil && a.cfg.Feishu.Quiet
}

func quietModeStatusText(enabled bool) string {
	if enabled {
		return "开启"
	}
	return "关闭"
}

func shouldDeliverTurnKindInQuiet(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "final_message", "turn_output", "turn_plan", "turn_queued", "turn_terminal":
		return true
	default:
		return false
	}
}

func shouldDeliverTurnSnapshotInQuiet(snapshot turnItemSnapshot) bool {
	switch normalizeTurnItemType(snapshot.ItemType) {
	case "plan":
		return true
	case "agent_message":
		return true
	default:
		return false
	}
}

func (a *App) renderQuietModeCard() map[string]any {
	return a.renderQuietModeMenuCard("")
}

func (a *App) renderQuietModeMenuCard(sessionKey string) map[string]any {
	enabled := a.quietModeEnabled()
	body := "当前状态: `" + quietModeStatusText(enabled) + "`\n\n开启后，会静默工具调用类消息和过程回复（例如 command execution、file change、web search、其它 tool/event），但仍保留 agent message、final answer、plan、排队/终态状态消息，以及 approval 请求。"
	buttons := []feishu.Button{
		{
			Text: func() string {
				if enabled {
					return "当前 · 开启"
				}
				return "开启"
			}(),
			Type: func() string {
				if enabled {
					return "primary"
				}
				return "default"
			}(),
			Value: map[string]any{"action": "quiet.set", "enabled": true, "session_key": sessionKey},
		},
		{
			Text: func() string {
				if !enabled {
					return "当前 · 关闭"
				}
				return "关闭"
			}(),
			Type: func() string {
				if !enabled {
					return "primary"
				}
				return "default"
			}(),
			Value: map[string]any{"action": "quiet.set", "enabled": false, "session_key": sessionKey},
		},
	}
	if strings.TrimSpace(sessionKey) != "" {
		buttons = append(buttons, feishu.Button{
			Text:  "返回上一级",
			Type:  "default",
			Value: map[string]any{"action": "menu.group.session", "session_key": sessionKey},
		})
	}
	return a.feishu.SimpleStatusCard("Quiet Mode", "blue", menuCardBody("menu.quiet", body), buttons)
}

func (a *App) updateQuietMode(enabled bool) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("nil config")
	}
	a.cfg.Feishu.Quiet = enabled
	if err := a.cfg.Normalize(filepath.Dir(a.cfgPath)); err != nil {
		return err
	}
	return config.Save(a.cfgPath, a.cfg)
}

func (a *App) commandQuiet(msg *feishu.InboundMessage, args []string) error {
	enabled := !a.quietModeEnabled()
	if len(args) > 0 {
		switch strings.TrimSpace(args[0]) {
		case "on":
			enabled = true
		case "off":
			enabled = false
		default:
			return fmt.Errorf("usage: /quiet | /quiet on | /quiet off")
		}
	}
	if err := a.updateQuietMode(enabled); err != nil {
		return err
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "Quiet Mode 已切换为 `"+quietModeStatusText(enabled)+"`。", msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}
