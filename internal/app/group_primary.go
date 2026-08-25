package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"feidex/internal/app/appstate"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type botGroupAddedConfigurer interface {
	SetBotGroupAddedHandler(func(*feishu.BotGroupEvent))
}

func configureGroupPrimaryEvents(a *App) {
	if a == nil || a.feishu == nil {
		return
	}
	configurer, ok := a.feishu.(botGroupAddedConfigurer)
	if !ok {
		return
	}
	configurer.SetBotGroupAddedHandler(func(event *feishu.BotGroupEvent) {
		handleBotGroupAdded(a, event)
	})
}

func handleBotGroupAdded(a *App, event *feishu.BotGroupEvent) {
	if a == nil || event == nil {
		return
	}
	chatID := strings.TrimSpace(event.ChatID)
	if chatID == "" {
		return
	}
	if _, err := ensureGroupPrimaryInitialized(context.Background(), a, "group", chatID); err != nil {
		slog.Warn("group primary auto init failed after bot added",
			"frontend_id", strings.TrimSpace(a.FrontendID()),
			"chat_id", chatID,
			"error", err,
		)
	}
}

func ensureGroupPrimaryInitialized(ctx context.Context, a *App, chatType, chatID string) (*state.GroupPrimary, error) {
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	chatID = strings.TrimSpace(chatID)
	if a == nil || chatType != "group" || chatID == "" {
		return nil, nil
	}
	if primary := groupPrimaryForChat(a, chatType, chatID); primary != nil {
		return primary, nil
	}
	if a.feishu == nil {
		return nil, fmt.Errorf("feishu client not initialized")
	}
	botCount, err := a.feishu.GetGroupBotCount(ctx, chatID)
	if err != nil {
		return nil, err
	}
	return setGroupPrimary(a, chatType, chatID, botCount == 1)
}

func groupPrimaryForChat(a *App, chatType, chatID string) *state.GroupPrimary {
	if a == nil {
		return nil
	}
	return a.State().GroupPrimary(chatType, chatID)
}

func hasGroupPrimaryState(a *App, chatType, chatID string) bool {
	return groupPrimaryForChat(a, chatType, chatID) != nil
}

func isGroupPrimary(a *App, chatType, chatID string) bool {
	primary := groupPrimaryForChat(a, chatType, chatID)
	return primary != nil && primary.Primary
}

func setGroupPrimary(a *App, chatType, chatID string, primary bool) (*state.GroupPrimary, error) {
	if a == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	chatID = strings.TrimSpace(chatID)
	if chatType != "group" || chatID == "" {
		return nil, fmt.Errorf("group chat is required")
	}
	record := groupPrimaryForChat(a, chatType, chatID)
	if record == nil {
		record = &state.GroupPrimary{
			ID:         appstate.DefaultGroupPrimaryID(a.FrontendID(), chatType, chatID),
			FrontendID: a.FrontendID(),
			ChatID:     chatID,
			ChatType:   chatType,
		}
	}
	record.Primary = primary
	if err := a.State().SaveGroupPrimary(record); err != nil {
		return nil, err
	}
	if primary {
		if err := clearOtherGroupPrimariesForChat(a, chatType, chatID, record.ID); err != nil {
			return nil, err
		}
	}
	updated := groupPrimaryForChat(a, chatType, chatID)
	if updated == nil {
		return nil, fmt.Errorf("group primary state for %s/%s not found after update", chatType, chatID)
	}
	return updated, nil
}

func clearOtherGroupPrimariesForChat(a *App, chatType, chatID, keepID string) error {
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	chatID = strings.TrimSpace(chatID)
	keepID = strings.TrimSpace(keepID)
	if a == nil || a.Store() == nil || chatID == "" || keepID == "" {
		return nil
	}
	for _, primary := range a.Store().AllGroupPrimaries() {
		if primary == nil || strings.TrimSpace(primary.ID) == keepID || !primary.Primary {
			continue
		}
		if strings.ToLower(strings.TrimSpace(primary.ChatType)) != chatType || strings.TrimSpace(primary.ChatID) != chatID {
			continue
		}
		primary.Primary = false
		if err := a.Store().UpsertGroupPrimary(primary); err != nil {
			return err
		}
	}
	return nil
}
