package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/app/cardactions"
	appruntime "feidex/internal/app/runtime"
	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const serviceTierFast = appruntime.ServiceTierFast

var normalizeServiceTier = appruntime.NormalizeServiceTier

var toggleServiceTier = appruntime.ToggleServiceTier

var renderServiceTierValue = appruntime.RenderServiceTierValue

var renderServiceTierReplyValue = appruntime.RenderServiceTierReplyValue

func renderServiceTierMenuCard(a *App, sessionKey string) map[string]any {
	sess := a.State().Session(sessionKey)
	body := "配置当前 thread 的 service tier。"
	buttons := []feishu.Button{}
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		body += "\n\n当前没有活动线程。"
	} else {
		current := normalizeServiceTier(sess.ActiveThreadServiceTier)
		body += "\n\n当前线程: " + appthreadmenu.SessionCurrentThreadLabel(sess)
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
				Value: cardactions.ThreadActionValue{
					Action:     "service_tier.set",
					SessionKey: sessionKey,
					ThreadID:   sess.ActiveThreadID,
				}.Map(),
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
				Value: cardactions.ThreadActionValue{
					Action:      "service_tier.set",
					SessionKey:  sessionKey,
					ThreadID:    sess.ActiveThreadID,
					ServiceTier: serviceTierFast,
				}.Map(),
			},
		)
	}
	buttons = append(buttons, feishu.Button{
		Text:  "返回上一级",
		Type:  "default",
		Value: cardactions.MenuActionValue{Action: "menu.group.model", SessionKey: sessionKey}.Map(),
	})
	return a.feishu.SimpleStatusCard("Service Tier", "blue", menuCardBody("menu.fast", body), buttons)
}

func setThreadServiceTier(a *App, sessionKey, threadID, serviceTier string) (*state.Session, error) {
	appState := a.State()
	sess := appState.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程，无法切换 service tier")
	}
	if strings.TrimSpace(threadID) != "" && strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return nil, fmt.Errorf("当前 thread 已失效")
	}
	sess.ActiveThreadServiceTier = normalizeServiceTier(serviceTier)
	if err := appState.SaveSession(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func commandFast(a *App, msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		if msg == nil {
			return nil
		}
		sessionKey := makeSessionKey(a, msg)
		appState := a.State()
		sess := appState.Session(sessionKey)
		if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
			return fmt.Errorf("当前没有活动线程，无法切换 service tier")
		}
		next := toggleServiceTier(sess.ActiveThreadServiceTier)
		sess.ActiveThreadServiceTier = next
		if err := appState.SaveSession(sess); err != nil {
			return err
		}
		return a.feishu.ReplyText(context.Background(), msg.MessageID, "当前 thread ServiceTier 已切换为 "+renderServiceTierReplyValue(next)+"。", replyInThreadEnabled(a, msg.ChatType))
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: /fast | /fast fast | /fast default | /fast toggle | /fast config")
	}
	switch strings.TrimSpace(args[0]) {
	case "config", "fast", "default", "off", "toggle":
	default:
		return fmt.Errorf("usage: /fast | /fast fast | /fast default | /fast toggle | /fast config")
	}
	if msg == nil {
		return nil
	}
	sessionKey := makeSessionKey(a, msg)
	if strings.TrimSpace(args[0]) == "config" {
		card := renderServiceTierMenuCard(a, sessionKey)
		_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(a, msg.ChatType))
		return err
	}
	appState := a.State()
	sess := appState.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return fmt.Errorf("当前没有活动线程，无法切换 service tier")
	}
	next := ""
	switch strings.TrimSpace(args[0]) {
	case "fast":
		next = serviceTierFast
	case "default", "off":
		next = ""
	case "toggle":
		next = toggleServiceTier(sess.ActiveThreadServiceTier)
	}
	sess.ActiveThreadServiceTier = next
	if err := appState.SaveSession(sess); err != nil {
		return err
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "当前 thread ServiceTier 已切换为 "+renderServiceTierReplyValue(next)+"。", replyInThreadEnabled(a, msg.ChatType))
}
