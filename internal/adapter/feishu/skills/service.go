// Package skills adapts slash commands and card actions to the skill use case.
package skills

import (
	"context"
	"errors"
	menuutil "feidex/internal/adapter/feishu/menuutil"
	skillapp "feidex/internal/application/skill"
	"feidex/internal/feishu"
	"fmt"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	"log/slog"
	"strings"
	"time"
)

type Outbound interface {
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
	PatchCard(context.Context, string, map[string]any) error
}

type Service struct {
	*skillapp.Service
	Outbound             Outbound
	MakeSessionKey       func(*feishu.InboundMessage) string
	ReplyInThreadEnabled func(string) bool
	FormatMenuBody       func(string, string) string
	CommandLabel         func(string, string) string
	RunAsync             func(string, func()) bool
}

const skillConfigReloadArg = "reload"

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

// CommandSkills handles the /skills command.
func (s *Service) CommandSkills(msg *feishu.InboundMessage, args []string) error {
	forceReload := false
	switch len(args) {
	case 0:
	case 1:
		if strings.TrimSpace(args[0]) != skillConfigReloadArg {
			return fmt.Errorf("usage: /skills | /skills reload")
		}
		forceReload = true
	default:
		return fmt.Errorf("usage: /skills | /skills reload")
	}
	card, err := s.RenderSkillsCard(s.MakeSessionKey(msg), forceReload)
	if err != nil {
		return err
	}
	_, err = s.Outbound.ReplyCard(s.Context(), msg.MessageID, card, s.ReplyInThreadEnabled(msg.ChatType))
	return err
}

// RenderSkillsCard renders the skills list card for the given session.
func (s *Service) RenderSkillsCard(sessionKey string, forceReload bool) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(s.Context(), 20*time.Second)
	defer cancel()
	view, err := s.Snapshot(ctx, sessionKey, forceReload)
	if err != nil {
		return nil, err
	}
	card := BuildCard(BuildCardParams{
		Entry:       view.Entry,
		HasPending:  view.HasPending,
		Pending:     view.Pending,
		SessionKey:  sessionKey,
		FormatBody:  func(body string) string { return s.FormatMenuBody("menu.skills", body) },
		ReloadLabel: s.CommandLabel("刷新", "/skills reload"),
	})
	return card, nil
}

// CompleteSkillsReload handles the skills.reload card action.
func (s *Service) CompleteSkillsReload(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.completeAction(action, sessionKey, "正在刷新 skill 列表", func() (*callback.CardActionTriggerResponse, error) {
		return s.completeReload(sessionKey)
	})
}

func (s *Service) completeReload(sessionKey string) (*callback.CardActionTriggerResponse, error) {
	card, err := s.RenderSkillsCard(sessionKey, true)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已刷新 skill 列表"},
		Card:  rawCard(card),
	}, nil
}

// CompleteSkillsSelect handles the skills.select card action.
func (s *Service) CompleteSkillsSelect(action *feishu.CardAction, sessionKey, selectedValue string) (*callback.CardActionTriggerResponse, error) {
	return s.completeAction(action, sessionKey, "正在选择 skill", func() (*callback.CardActionTriggerResponse, error) {
		return s.completeSelect(sessionKey, selectedValue)
	})
}

func (s *Service) completeSelect(sessionKey, selectedValue string) (*callback.CardActionTriggerResponse, error) {
	ctx, cancel := context.WithTimeout(s.Context(), 20*time.Second)
	defer cancel()
	selected, err := s.Select(ctx, sessionKey, selectedValue)
	if errors.Is(err, skillapp.ErrDisabled) {
		card, renderErr := s.RenderSkillsCard(sessionKey, false)
		if renderErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "该 skill 当前为 disabled，不能用于下一条消息"},
			Card:  rawCard(card),
		}, nil
	}
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	card, err := s.RenderSkillsCard(sessionKey, false)
	if err != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: PendingConfirmationText(selected.Name)},
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: PendingConfirmationText(selected.Name)},
		Card:  rawCard(card),
	}, nil
}

func (s *Service) CompleteSkillsOpen(action *feishu.CardAction, key string) (*callback.CardActionTriggerResponse, error) {
	return s.completeAction(action, key, "正在读取 skill 列表", func() (*callback.CardActionTriggerResponse, error) {
		card, err := s.RenderSkillsCard(key, false)
		if err != nil {
			return nil, err
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "已打开 skill 列表"}, Card: rawCard(card)}, nil
	})
}

// Only strings are captured from the callback. Slow catalog reads and the
// resulting patch are admitted by the frontend runtime after immediate ack.
func (s *Service) completeAction(action *feishu.CardAction, key, text string, work func() (*callback.CardActionTriggerResponse, error)) (*callback.CardActionTriggerResponse, error) {
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return work()
	}
	messageID := strings.TrimSpace(action.MessageID)
	if s.RunAsync == nil || !s.RunAsync(key, func() {
		response, err := work()
		var card map[string]any
		if response != nil && response.Card != nil {
			card, _ = response.Card.Data.(map[string]any)
		}
		if card == nil {
			notice := "操作没有返回卡片"
			if response != nil && response.Toast != nil {
				notice = response.Toast.Content
			}
			if err != nil {
				notice = err.Error()
			}
			card = s.noticeCard(key, notice)
		}
		if err := s.Outbound.PatchCard(s.Context(), messageID, card); err != nil {
			slog.Warn("skills card patch failed", "message_id", messageID, "error", err)
		}
	}) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 frontend 正在停止"}}, nil
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: text}, Card: rawCard(s.noticeCard(key, text))}, nil
}

func (s *Service) noticeCard(key, text string) map[string]any {
	return menuutil.MarkdownPageCard{
		Node: "menu.skills", SessionKey: key, Title: "技能列表", Color: "blue",
		Body: text,
	}.Render()
}
