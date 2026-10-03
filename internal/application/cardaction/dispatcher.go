package cardaction

import (
	"fmt"
	"log/slog"
	"strings"

	"feidex/internal/application"
)

type Handler func(application.CardAction) (any, error)

type Dependencies struct {
	NormalizeSessionKey func(*application.CardAction)
	ResolveActionName   func(application.CardAction) string
	BlockedReason       func(string) string
	Handlers            map[string]Handler
}

type Service struct{ deps Dependencies }

func NewService(deps Dependencies) Service { return Service{deps: deps} }

func (s Service) Dispatch(action application.CardAction) (any, error) {
	if action.ActionValue.Empty() && action.Name == "" && action.MessageID == "" {
		return nil, nil
	}
	if s.deps.NormalizeSessionKey != nil {
		s.deps.NormalizeSessionKey(&action)
	}
	name := ""
	if s.deps.ResolveActionName != nil {
		name = strings.TrimSpace(s.deps.ResolveActionName(action))
	}
	if s.deps.BlockedReason != nil {
		if reason := s.deps.BlockedReason(name); strings.TrimSpace(reason) != "" {
			return application.CardActionResult{ToastType: "warning", ToastContent: reason}, nil
		}
	}
	handler := s.deps.Handlers[name]
	if handler == nil {
		slog.Warn("unknown feishu card action", "name", name, "raw_name", action.Name, "message_id", action.MessageID, "chat_id", action.ChatID, "user_id", action.UserID, "action_value", fmt.Sprintf("%v", action.ActionValue), "form_value", fmt.Sprintf("%v", action.FormValue))
		return application.CardActionResult{ToastType: "warning", ToastContent: "未知操作"}, nil
	}
	return handler(action)
}
