package servicetiercmd

import (
	"context"
	"fmt"
	"strings"

	appcore "feidex/internal/app/appcore"
	appruntime "feidex/internal/app/runtime"
	appthreadview "feidex/internal/app/threadview"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const (
	ServiceTierFast = appruntime.ServiceTierFast

	commandFastUsage = "usage: /fast | /fast fast | /fast default | /fast toggle | /fast config"
)

var NormalizeServiceTier = appruntime.NormalizeServiceTier

var ToggleServiceTier = appruntime.ToggleServiceTier

var RenderServiceTierValue = appruntime.RenderServiceTierValue

var RenderServiceTierReplyValue = appruntime.RenderServiceTierReplyValue

type App interface {
	appcore.AppConfig

	Feishu() appcore.FeishuClient
	ServiceTierAppState() AppStateProvider
	MenuCardBody(action, body string) string
}

type AppStateProvider interface {
	Session(key string) *state.Session
	SaveSession(sess *state.Session) error
}

type Service struct {
	app App
}

func NewService(app App) Service {
	return Service{app: app}
}

func (s Service) RenderMenuCard(sessionKey string) map[string]any {
	stateProvider := s.app.ServiceTierAppState()
	sess := stateProvider.Session(sessionKey)
	body := "配置当前 thread 的 service tier。"
	buttons := []feishu.Button{}
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		body += "\n\n当前没有活动线程。"
	} else {
		current := NormalizeServiceTier(sess.ActiveThreadServiceTier)
		body += "\n\n当前线程: " + appthreadview.CurrentThreadLabel(sess.ActiveThreadName, sess.ActiveThreadPreview, sess.ActiveThreadID)
		body += "\n当前值: " + RenderServiceTierValue(current)
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
				Value: map[string]any{
					"action":      "service_tier.set",
					"session_key": sessionKey,
					"thread_id":   sess.ActiveThreadID,
				},
			},
			feishu.Button{
				Text: func() string {
					if current == ServiceTierFast {
						return "当前 · fast"
					}
					return "fast"
				}(),
				Type: func() string {
					if current == ServiceTierFast {
						return "primary"
					}
					return "default"
				}(),
				Value: map[string]any{
					"action":       "service_tier.set",
					"session_key":  sessionKey,
					"thread_id":    sess.ActiveThreadID,
					"service_tier": ServiceTierFast,
				},
			},
		)
	}
	buttons = append(buttons, feishu.Button{
		Text: "返回上一级",
		Type: "default",
		Value: map[string]any{
			"action":      "menu.group.model",
			"session_key": sessionKey,
		},
	})
	return s.app.Feishu().SimpleStatusCard("Service Tier", "blue", s.app.MenuCardBody("menu.fast", body), buttons)
}

func (s Service) SetThreadServiceTier(sessionKey, threadID, serviceTier string) (*state.Session, error) {
	stateProvider := s.app.ServiceTierAppState()
	sess := stateProvider.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程，无法切换 service tier")
	}
	if strings.TrimSpace(threadID) != "" && strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return nil, fmt.Errorf("当前 thread 已失效")
	}
	sess.ActiveThreadServiceTier = NormalizeServiceTier(serviceTier)
	if err := stateProvider.SaveSession(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s Service) CommandFast(msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		return s.toggleServiceTier(msg)
	}
	if len(args) > 1 {
		return fmt.Errorf(commandFastUsage)
	}
	switch strings.TrimSpace(args[0]) {
	case "config", "fast", "default", "off", "toggle":
	default:
		return fmt.Errorf(commandFastUsage)
	}
	if msg == nil {
		return nil
	}
	sessionKey := appcore.MakeSessionKey(s.app, msg)
	if strings.TrimSpace(args[0]) == "config" {
		_, err := s.app.Feishu().ReplyCard(context.Background(), msg.MessageID, s.RenderMenuCard(sessionKey), appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
		return err
	}
	stateProvider := s.app.ServiceTierAppState()
	sess := stateProvider.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return fmt.Errorf("当前没有活动线程，无法切换 service tier")
	}
	next := ""
	switch strings.TrimSpace(args[0]) {
	case "fast":
		next = ServiceTierFast
	case "default", "off":
		next = ""
	case "toggle":
		next = ToggleServiceTier(sess.ActiveThreadServiceTier)
	}
	sess.ActiveThreadServiceTier = next
	if err := stateProvider.SaveSession(sess); err != nil {
		return err
	}
	return s.app.Feishu().ReplyText(context.Background(), msg.MessageID, "当前 thread ServiceTier 已切换为 "+RenderServiceTierReplyValue(next)+"。", appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
}

func (s Service) toggleServiceTier(msg *feishu.InboundMessage) error {
	if msg == nil {
		return nil
	}
	sessionKey := appcore.MakeSessionKey(s.app, msg)
	stateProvider := s.app.ServiceTierAppState()
	sess := stateProvider.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return fmt.Errorf("当前没有活动线程，无法切换 service tier")
	}
	next := ToggleServiceTier(sess.ActiveThreadServiceTier)
	sess.ActiveThreadServiceTier = next
	if err := stateProvider.SaveSession(sess); err != nil {
		return err
	}
	return s.app.Feishu().ReplyText(context.Background(), msg.MessageID, "当前 thread ServiceTier 已切换为 "+RenderServiceTierReplyValue(next)+"。", appcore.ReplyInThreadEnabled(s.app, msg.ChatType))
}
