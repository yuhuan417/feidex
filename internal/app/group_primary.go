package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	statejson "feidex/internal/adapter/storage/json"
	approuting "feidex/internal/application/routing"
	"feidex/internal/domain/identity"
	domainrouting "feidex/internal/domain/routing"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type botGroupAddedConfigurer interface {
	SetBotGroupAddedHandler(func(*feishu.BotGroupEvent))
}

type botOpenIDProvider interface {
	BotOpenID() string
}

type liveBotOpenIDProvider interface {
	RefreshBotOpenID() string
}

type botNameProvider interface {
	BotName() string
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
	if _, err := ensureGroupPrimaryInitialized(a.Context(), a, "group", chatID); err != nil {
		slog.Warn("group primary auto init failed after bot added",
			"frontend_id", strings.TrimSpace(a.FrontendID()),
			"chat_id", chatID,
			"error", err,
		)
	}
	// The bot is in this chat again, so undo any earlier "no longer a member"
	// mark; otherwise its announcement would stay disabled forever.
	clearGroupAnnouncementBotAbsent(a, chatID)
	scheduleGroupAnnouncementStatusRefresh(a, chatID, "bot_added")
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
	enabled := false
	if botCount == 1 {
		if currentLiveBotOpenID(a) == "" {
			return nil, fmt.Errorf("bot open_id is required to initialize group primary")
		}
		enabled = true
	}
	// The group lookup can finish after this frontend has initialized its local
	// state or processed /primary on. EnsureGroupPrimary preserves that state.
	result, err := (approuting.Service{Repository: statejson.NewGroupPrimaryRepository(a.Store(), a.FrontendID())}).EnsurePrimary(a.FrontendID(), chatType, chatID, enabled)
	if err != nil || result == nil {
		return nil, err
	}
	return &state.GroupPrimary{FrontendID: result.FrontendID, ChatID: result.ChatID, ChatType: result.ChatType, Enabled: result.Enabled, LastAssignmentMessageID: result.LastAssignmentMessageID, LastAssignmentCreatedAt: result.LastAssignmentCreatedAt}, nil
}

func groupPrimaryForChat(a *App, chatType, chatID string) *state.GroupPrimary {
	if a == nil || a.Store() == nil {
		return nil
	}
	repository := statejson.NewGroupPrimaryRepository(a.Store(), a.FrontendID())
	primary, err := repository.GetGroupPrimary(a.FrontendID(), chatType, chatID)
	if err != nil || primary == nil {
		return nil
	}
	return &state.GroupPrimary{
		ID:                      statejson.GroupPrimaryID(primary.FrontendID, primary.ChatType, primary.ChatID),
		FrontendID:              primary.FrontendID,
		ChatID:                  primary.ChatID,
		ChatType:                primary.ChatType,
		Enabled:                 primary.Enabled,
		LastAssignmentMessageID: primary.LastAssignmentMessageID,
		LastAssignmentCreatedAt: primary.LastAssignmentCreatedAt,
	}
}

func hasGroupPrimaryState(a *App, chatType, chatID string) bool {
	return groupPrimaryForChat(a, chatType, chatID) != nil
}

func isGroupPrimary(a *App, chatType, chatID string) bool {
	primary := groupPrimaryForChat(a, chatType, chatID)
	return primary != nil && primary.Enabled
}

func currentBotOpenID(a *App) string {
	if a == nil || a.feishu == nil {
		return ""
	}
	provider, ok := a.feishu.(botOpenIDProvider)
	if !ok {
		return ""
	}
	return strings.TrimSpace(provider.BotOpenID())
}

func currentLiveBotOpenID(a *App) string {
	if a == nil || a.feishu == nil {
		return ""
	}
	if provider, ok := a.feishu.(liveBotOpenIDProvider); ok {
		if openID := strings.TrimSpace(provider.RefreshBotOpenID()); openID != "" {
			return openID
		}
	}
	return currentBotOpenID(a)
}

func currentBotName(a *App) string {
	if a == nil || a.feishu == nil {
		return ""
	}
	provider, ok := a.feishu.(botNameProvider)
	if !ok {
		return ""
	}
	return strings.TrimSpace(provider.BotName())
}

func currentBotDisplayName(a *App) string {
	if name := currentBotName(a); name != "" {
		return name
	}
	return currentBotOpenID(a)
}

func setGroupPrimaryState(a *App, chatType, chatID string, enabled bool, assignment *feishu.InboundMessage) (*state.GroupPrimary, error) {
	if a == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	chatID = strings.TrimSpace(chatID)
	if chatType != "group" || chatID == "" {
		return nil, fmt.Errorf("group chat is required")
	}
	input := approuting.ChangePrimary{
		Frontend: identity.FrontendID(a.FrontendID()),
		Chat:     identity.ChatRef{Type: identity.ChatType(chatType), ID: chatID},
		Enabled:  enabled,
	}
	if assignment != nil {
		input.Assignment = &domainrouting.AssignmentStamp{MessageID: assignment.MessageID, CreatedAt: assignment.CreatedAt}
	}
	if _, err := (approuting.Service{Repository: statejson.NewGroupPrimaryRepository(a.Store(), a.FrontendID())}).SetPrimary(input); err != nil {
		return nil, err
	}
	updated := groupPrimaryForChat(a, chatType, chatID)
	if updated == nil {
		return nil, fmt.Errorf("group primary state for %s/%s not found after update", chatType, chatID)
	}
	slog.Info("group primary state written",
		"frontend_id", strings.TrimSpace(a.FrontendID()),
		"chat_id", chatID,
		"primary_enabled", updated.Enabled,
		"assignment_message_id", strings.TrimSpace(updated.LastAssignmentMessageID),
	)
	return updated, nil
}

func setGroupPrimary(a *App, chatType, chatID string, enabled bool) (*state.GroupPrimary, error) {
	return setGroupPrimaryState(a, chatType, chatID, enabled, nil)
}

func syncGroupPrimaryAssignment(a *App, msg *feishu.InboundMessage) (bool, error) {
	assignment, ok := groupPrimaryAssignmentFromMessage(msg)
	if !ok {
		return false, nil
	}
	selfOpenID := currentLiveBotOpenID(a)
	if selfOpenID == "" {
		slog.Warn("group primary assignment skipped without live bot identity",
			"frontend_id", strings.TrimSpace(a.FrontendID()),
			"chat_id", msg.ChatID,
			"message_id", msg.MessageID,
		)
		return true, nil
	}
	if record := groupPrimaryForChat(a, msg.ChatType, msg.ChatID); staleGroupPrimaryAssignment(record, msg) {
		return true, nil
	}
	enabled := selfOpenID == assignment.TargetBotOpenID
	if enabled {
		// Let the target frontend continue through the normal command handler so
		// it can acknowledge the assignment.
		return false, nil
	}
	_, err := setGroupPrimaryState(a, msg.ChatType, msg.ChatID, false, msg)
	return true, err
}

func isGroupPrimaryControlMessage(msg *feishu.InboundMessage) bool {
	if msg == nil || strings.TrimSpace(msg.ChatType) != "group" {
		return false
	}
	return domainrouting.ParsePrimaryOnCommand(msg.Text) || domainrouting.ParseEmptyBotMention(msg.Text)
}

func staleGroupPrimaryAssignment(record *state.GroupPrimary, assignment *feishu.InboundMessage) bool {
	if record == nil || assignment == nil {
		return false
	}
	return domainrouting.StaleAssignment(
		record.LastAssignmentMessageID,
		record.LastAssignmentCreatedAt,
		assignment.MessageID,
		assignment.CreatedAt,
	)
}

func groupPrimaryAssignmentFromMessage(msg *feishu.InboundMessage) (domainrouting.GroupPrimaryAssignment, bool) {
	if msg == nil || strings.TrimSpace(msg.ChatType) != "group" {
		return domainrouting.GroupPrimaryAssignment{}, false
	}
	return domainrouting.ParseGroupPrimaryAssignment(msg.Text, msg.MentionedOpenIDs)
}

func groupPrimaryAssignmentForCommand(msg *feishu.InboundMessage) (domainrouting.GroupPrimaryAssignment, bool) {
	if msg == nil || strings.TrimSpace(msg.ChatType) != "group" {
		return domainrouting.GroupPrimaryAssignment{}, false
	}
	return domainrouting.ParseGroupPrimaryAssignment("/primary on", msg.MentionedOpenIDs)
}
