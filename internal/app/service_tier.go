package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

const (
	serviceTierFast = "fast"
)

func normalizeServiceTier(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case serviceTierFast:
		return serviceTierFast
	default:
		return ""
	}
}

func toggleServiceTier(value string) string {
	if normalizeServiceTier(value) == serviceTierFast {
		return ""
	}
	return serviceTierFast
}

func renderServiceTierValue(value string) string {
	value = normalizeServiceTier(value)
	if value == "" {
		return "-"
	}
	return "`" + value + "`"
}

func renderServiceTierReplyValue(value string) string {
	value = normalizeServiceTier(value)
	if value == "" {
		return "未设置"
	}
	return "`" + value + "`"
}

func (a *App) renderServiceTierMenuCard(sessionKey string) map[string]any {
	sess := a.appState().session(sessionKey)
	body := "配置当前 thread 的 service tier。"
	buttons := []feishu.Button{}
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		body += "\n\n当前没有活动线程。"
	} else {
		current := normalizeServiceTier(sess.ActiveThreadServiceTier)
		body += "\n\n当前线程: " + currentThreadLabel(sess)
		body += "\n当前值: " + renderServiceTierValue(current)
		buttons = append(buttons,
			feishu.Button{
				Text: func() string {
					if current == "" {
						return "当前 · 默认"
					}
					return "默认"
				}(),
				Type: func() string {
					if current == "" {
						return "primary"
					}
					return "default"
				}(),
				Value: map[string]any{"action": "service_tier.set", "session_key": sessionKey, "thread_id": sess.ActiveThreadID, "service_tier": ""},
			},
			feishu.Button{
				Text: func() string {
					if current == serviceTierFast {
						return "当前 · fast"
					}
					return "fast"
				}(),
				Type: func() string {
					if current == serviceTierFast {
						return "primary"
					}
					return "default"
				}(),
				Value: map[string]any{"action": "service_tier.set", "session_key": sessionKey, "thread_id": sess.ActiveThreadID, "service_tier": serviceTierFast},
			},
		)
	}
	buttons = append(buttons, feishu.Button{
		Text:  "返回上一级",
		Type:  "default",
		Value: map[string]any{"action": "menu.group.model", "session_key": sessionKey},
	})
	return a.feishu.SimpleStatusCard("Service Tier", "blue", menuCardBody("menu.fast", body), buttons)
}

func (a *App) setThreadServiceTier(sessionKey, threadID, serviceTier string) (*state.Session, error) {
	appState := a.appState()
	sess := appState.session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程，无法切换 service tier")
	}
	if strings.TrimSpace(threadID) != "" && strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return nil, fmt.Errorf("当前 thread 已失效")
	}
	sess.ActiveThreadServiceTier = normalizeServiceTier(serviceTier)
	if err := appState.saveSession(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func (a *App) commandFast(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /fast")
	}
	if msg == nil {
		return nil
	}
	sessionKey := a.makeSessionKey(msg)
	appState := a.appState()
	sess := appState.session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return fmt.Errorf("当前没有活动线程，无法切换 service tier")
	}
	next := toggleServiceTier(sess.ActiveThreadServiceTier)
	sess.ActiveThreadServiceTier = next
	if err := appState.saveSession(sess); err != nil {
		return err
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "当前 thread ServiceTier 已切换为 "+renderServiceTierReplyValue(next)+"。", msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}
