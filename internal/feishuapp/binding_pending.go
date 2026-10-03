package feishuapp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/application/routing"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/state"
)

func newBindingPendingService(a *App) routing.PendingService {
	configuration := newRoutingConfiguration(a).ConfigurationService
	return routing.PendingService{Configuration: configuration, Repository: a.State()}
}
func (s bindingService) gatePendingGroupMessage(msg *feishu.InboundMessage) (bool, error) {
	if s.app == nil || msg == nil || !isGroupMessage(msg) {
		return false, nil
	}
	result, err := newBindingPendingService(s.app).Gate(msg, makeSessionKey(s.app, msg), isGroupPrimary(s.app, msg.ChatType, msg.ChatID), time.Now().Unix())
	if err != nil || !result.Handled {
		return result.Handled, err
	}
	if err := newEffectRunner(s.app).Run(s.app.Context(), result.Effects); err != nil {
		return false, err
	}
	card := newWorkspaceRenderService(s.app).RenderWorkspaceMenuCard(makeSessionKey(s.app, msg))
	_, err = replyCardWithIDEffect(s.app.Context(), s.app, msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return true, err
}
func (s bindingService) replayPendingBindingMessage(binding *state.AgentBinding) error {
	if s.app == nil || !routing.BindingReady(binding) {
		return nil
	}
	worker := frontendruntime.BindingReplay{Service: newBindingPendingService(s.app), Runner: newEffectRunner(s.app), Actors: s.app.sessionActorRuntime()}
	return worker.Replay(s.app.Context(), binding.ID)
}

func (s bindingService) replayPendingBindingMessageAsync(binding *state.AgentBinding) {
	if binding == nil || len(binding.PendingMessages) == 0 {
		return
	}
	messageID := strings.TrimSpace(binding.PendingMessages[0].MessageID)
	runSessionAsync(s.app, binding.ChatID, func() {
		if err := s.replayPendingBindingMessage(binding); err != nil {
			slog.Warn("binding pending message replay failed", "binding_id", binding.ID, "message_id", messageID, "error", err)
			if messageID != "" {
				_ = replyTextByAnchorEffect(context.Background(), s.app, messageID, fmt.Sprintf("绑定成功，但处理原消息失败: %v", err), replyInThreadEnabled(s.app, binding.ChatType))
			}
		}
	})
}

func discardPendingBindingMessageByID(a *App, messageID string) bool {
	if a == nil || strings.TrimSpace(messageID) == "" || a.State() == nil {
		return false
	}
	discarded, err := newBindingPendingService(a).Discard(messageID)
	if err != nil {
		slog.Warn("discard pending binding message failed", "message_id", messageID, "error", err)
	}
	return discarded
}
