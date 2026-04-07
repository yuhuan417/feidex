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
	case "agent_message", "plan":
		return true
	default:
		return false
	}
}

func (a *App) renderQuietModeCard() map[string]any {
	enabled := a.quietModeEnabled()
	body := "当前状态: `" + quietModeStatusText(enabled) + "`\n\n开启后，只发送 agent message 和 approval 请求；工具调用、计划、排队等 turn 过程消息不再发到飞书。"
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
			Value: map[string]any{"action": "quiet.set", "enabled": true},
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
			Value: map[string]any{"action": "quiet.set", "enabled": false},
		},
	}
	return a.feishu.SimpleStatusCard("Quiet Mode", "blue", body, buttons)
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
	if len(args) > 0 {
		switch strings.TrimSpace(args[0]) {
		case "on":
			if err := a.updateQuietMode(true); err != nil {
				return err
			}
		case "off":
			if err := a.updateQuietMode(false); err != nil {
				return err
			}
		default:
			return fmt.Errorf("usage: /quiet | /quiet on | /quiet off")
		}
	}
	card := a.renderQuietModeCard()
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}
