// Package autoretry presents retry status and parses its Feishu command surface.
package autoretry

import (
	"context"
	"feidex/internal/adapter/feishu/menuutil"
	retry "feidex/internal/application/autoretry"
	"feidex/internal/feishu"
	"feidex/internal/textutil"
	"fmt"
	"strings"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type Outbound interface {
	PatchCard(context.Context, string, map[string]any) error
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
	SendCard(context.Context, string, map[string]any) (string, error)
}
type CardRenderer interface {
	SimpleStatusCard(string, string, string, []feishu.Button) map[string]any
}
type Settings struct {
	FrontendID, Backend, Title string
	Enabled                    bool
}
type Service struct {
	retry.Engine
	Context     func() context.Context
	Outbound    Outbound
	Renderer    CardRenderer
	Settings    func() Settings
	SessionKey  func(*feishu.InboundMessage) string
	ReplyAction func(*feishu.InboundMessage, *callback.CardActionTriggerResponse) error
}
type RetryState = retry.RetryState

var FormatDelay = retry.FormatDelay
var DelayForStep = retry.DelayForStep

func (s Service) context() context.Context {
	if s.Context != nil {
		return s.Context()
	}
	return context.Background()
}
func (s Service) AutoRetryTitle() string { return s.Settings().Title }
func (s Service) RetryStatus(snapshot RetryState, phase, notice string) string {
	if s.Outbound == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(s.context(), 5*time.Second)
	defer cancel()
	card := s.RenderAutoRetryLoopCard(snapshot, phase, notice)
	if snapshot.StatusMessageID != "" && s.Outbound.PatchCard(ctx, snapshot.StatusMessageID, card) == nil {
		return snapshot.StatusMessageID
	}
	var id string
	var err error
	if snapshot.TriggerMessageID != "" {
		id, err = s.Outbound.ReplyCard(ctx, snapshot.TriggerMessageID, card, false)
	} else if snapshot.ChatID != "" {
		id, err = s.Outbound.SendCard(ctx, snapshot.ChatID, card)
	}
	if err != nil {
		return ""
	}
	return id
}

// ActionSessionKey extracts the "session_key" value from a card action.
func ActionSessionKey(action *feishu.CardAction) string {
	if action == nil {
		return ""
	}
	value, _ := action.ActionValue["session_key"].(string)
	return strings.TrimSpace(value)
}

// RawCard wraps a card map in a callback.Card for card action responses.
func RawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

// CommandActionFromMessage builds a CardAction from an inbound message and
// optional action value overrides.
func CommandActionFromMessage(msg *feishu.InboundMessage, actionValue map[string]any) *feishu.CardAction {
	if actionValue == nil {
		actionValue = map[string]any{}
	}
	if msg == nil {
		return &feishu.CardAction{ActionValue: actionValue}
	}
	return &feishu.CardAction{
		ActionValue: actionValue,
		UserID:      strings.TrimSpace(msg.UserID),
		ChatID:      strings.TrimSpace(msg.ChatID),
		MessageID:   strings.TrimSpace(msg.MessageID),
	}
}

// RenderAutoRetryLoopCard builds the card for the retry loop status display.
func (s Service) RenderAutoRetryLoopCard(snapshot RetryState, phase, notice string) map[string]any {
	lines := []string{
		"当前线程: `" + textutil.FirstNonEmpty(strings.TrimSpace(snapshot.ThreadID), "-") + "`",
		"累计已重试: `" + fmt.Sprintf("%d", snapshot.RetryCount) + "` 次",
	}
	switch strings.TrimSpace(phase) {
	case "waiting":
		lines = append(lines,
			"下一次自动重试: 第 `"+fmt.Sprintf("%d", snapshot.RetryCount+1)+"` 次",
			"下一次间隔: `"+FormatDelay(DelayForStep(snapshot.BackoffStep))+"`",
		)
	case "running":
		lines = append(lines,
			"当前状态: 已发起第 `"+fmt.Sprintf("%d", snapshot.RetryCount)+"` 次自动重试",
		)
	}
	if text := strings.TrimSpace(notice); text != "" {
		lines = append([]string{text, ""}, lines...)
	}
	failure := textutil.FirstNonEmpty(strings.TrimSpace(snapshot.LastError), "后端未提供具体错误信息。")
	lines = append(lines, "", "最近一次失败原因:\n"+textutil.Truncate(failure, 2000))
	lines = append(lines, "", "如需终止，请发送 `/stop`。")
	color := "blue"
	switch strings.TrimSpace(phase) {
	case "waiting":
		color = "orange"
	case "running":
		color = "blue"
	case "completed":
		color = "green"
	case "interrupted", "stopped":
		color = "grey"
	}
	return s.Renderer.SimpleStatusCard(s.AutoRetryTitle(), color, strings.Join(lines, "\n"), nil)
}

// RenderAutoRetryConfigCard builds the configuration card for auto-retry.
func (s Service) RenderAutoRetryConfigCard(sessionKey string) map[string]any {
	enabled := s.AutoRetryEnabled()
	lines := []string{
		"当前 frontend: `" + textutil.FirstNonEmpty(strings.TrimSpace(s.Settings().FrontendID), "default") + "`",
		"当前 backend: `" + textutil.FirstNonEmpty(s.Settings().Backend, "unset") + "`",
		"开关状态: `" + map[bool]string{true: "on", false: "off"}[enabled] + "`",
		"",
		"当 turn 终态为 `failed` 且当前 session 仍保留活动线程时，会按“继续”自动重试。",
		"重试间隔会逐步增大，最长 `15s`。",
		"`/stop` 会停止当前 session 的自动重试流程。",
	}
	if snapshot, ok := s.CurrentAutoRetryState(sessionKey); ok {
		lines = append(lines,
			"",
			"当前 session 正在自动重试中。",
			"当前线程: `"+textutil.FirstNonEmpty(strings.TrimSpace(snapshot.ThreadID), "-")+"`",
			"累计已重试: `"+fmt.Sprintf("%d", snapshot.RetryCount)+"` 次",
		)
	}
	buttons := []feishu.Button{
		{
			Text: "开启",
			Type: func() string {
				if enabled {
					return "primary"
				}
				return "default"
			}(),
			Value: map[string]any{
				"action":      "auto_retry.set",
				"enabled":     "on",
				"session_key": sessionKey,
			},
		},
		{
			Text: "关闭",
			Type: func() string {
				if !enabled {
					return "primary"
				}
				return "default"
			}(),
			Value: map[string]any{
				"action":      "auto_retry.set",
				"enabled":     "off",
				"session_key": sessionKey,
			},
		},
	}
	return menuutil.PageCard{
		Node: "menu.auto_retry", SessionKey: sessionKey, Title: s.AutoRetryTitle(), Color: "blue",
		Body: strings.Join(lines, "\n"), Buttons: buttons,
	}.Render()
}

// CompleteAutoRetrySet handles the card action to toggle auto-retry on or off.
func (s Service) CompleteAutoRetrySet(action *feishu.CardAction, enabled bool) (*callback.CardActionTriggerResponse, error) {
	sessionKey := ActionSessionKey(action)
	if err := s.UpdateAutoRetryEnabled(enabled); err != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "error", Content: err.Error()},
			Card:  RawCard(s.RenderAutoRetryConfigCard(sessionKey)),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新自动重试"},
		Card:  RawCard(s.RenderAutoRetryConfigCard(sessionKey)),
	}, nil
}

// CommandAutoRetry handles /backend retry commands.
func (s Service) CommandAutoRetry(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: /backend retry | /backend retry status | /backend retry on | /backend retry off")
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "status" {
		card := s.RenderAutoRetryConfigCard(s.SessionKey(msg))
		_, err := s.Outbound.ReplyCard(s.context(), msg.MessageID, card, false)
		return err
	}
	enabled := false
	switch strings.TrimSpace(args[0]) {
	case "on":
		enabled = true
	case "off":
		enabled = false
	default:
		return fmt.Errorf("usage: /backend retry | /backend retry status | /backend retry on | /backend retry off")
	}
	resp, err := s.CompleteAutoRetrySet(CommandActionFromMessage(msg, nil), enabled)
	if err != nil {
		return err
	}
	return s.ReplyAction(msg, resp)
}
