package servicetier

import (
	"context"
	"errors"
	"feidex/internal/adapter/feishu/menuutil"
	"feidex/internal/adapter/feishu/threadview"
	"feidex/internal/application"
	"feidex/internal/application/threadsettings"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
	appruntime "feidex/internal/runtime"
	"strings"
)

const ServiceTierFast = appruntime.ServiceTierFast
const commandFastUsage = "usage: /fast | /fast fast | /fast default | /fast toggle | /fast config"

var NormalizeServiceTier = appruntime.NormalizeServiceTier
var ToggleServiceTier = appruntime.ToggleServiceTier
var RenderServiceTierValue = appruntime.RenderServiceTierValue
var RenderServiceTierReplyValue = appruntime.RenderServiceTierReplyValue

type Outbound interface {
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
	ReplyText(context.Context, string, string, bool) error
}

type Service struct {
	threadsettings.Service
	Context    func() context.Context
	Outbound   Outbound
	SessionKey func(*application.InboundMessage) string
}

func (s Service) outbound() Outbound {
	return s.Outbound
}

func (s Service) context() context.Context {
	if s.Context != nil {
		return s.Context()
	}
	return context.Background()
}
func (s Service) RenderMenuCard(key string) map[string]any {
	return RenderMenuCard(key, s.Repository.Session(key))
}
func (s Service) CommandFast(msg *application.InboundMessage, args []string) error {
	if len(args) > 1 {
		return errors.New(commandFastUsage)
	}
	mode := "toggle"
	if len(args) == 1 {
		mode = strings.TrimSpace(args[0])
	}
	switch mode {
	case "config", "fast", "default", "off", "toggle":
	default:
		return errors.New(commandFastUsage)
	}
	if msg == nil {
		return nil
	}
	key := s.SessionKey(msg)
	if mode == "config" {
		_, err := s.outbound().ReplyCard(s.context(), msg.MessageID, s.RenderMenuCard(key), false)
		return err
	}
	var sess *conversation.Session
	var err error
	if mode == "toggle" {
		sess, err = s.Toggle(key)
	} else {
		tier := ""
		if mode == "fast" {
			tier = ServiceTierFast
		}
		sess, err = s.SetThreadServiceTier(key, "", tier)
	}
	if err != nil {
		return err
	}
	return s.outbound().ReplyText(s.context(), msg.MessageID, "当前 thread ServiceTier 已切换为 "+RenderServiceTierReplyValue(sess.ActiveThreadServiceTier)+"。", false)
}
func RenderMenuCard(sessionKey string, sess *conversation.Session) map[string]any {
	body := "配置当前 thread 的 service tier。"
	buttons := []feishu.Button{}
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		body += "\n\n当前没有活动线程。"
	} else {
		current := NormalizeServiceTier(sess.ActiveThreadServiceTier)
		body += "\n\n当前线程: " + threadview.CurrentThreadLabel(sess.ActiveThreadName, sess.ActiveThreadPreview, sess.ActiveThreadID)
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
	return menuutil.PageCard{
		Node: "menu.fast", SessionKey: sessionKey, Title: "响应速度", Color: "blue",
		Body: body, Buttons: buttons,
	}.Render()
}
