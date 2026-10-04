package feishuapp

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
	if _, err := initializeGroupPrimary(a.Context(), a.bindings.PrimaryInitialization, a.FrontendID(), a.feishu, "group", chatID); err != nil {
		slog.Warn("group primary auto init failed after bot added",
			"frontend_id", strings.TrimSpace(a.FrontendID()),
			"chat_id", chatID,
			"error", err,
		)
	}
	// The bot is in this chat again, so undo any earlier "no longer a member"
	// mark; otherwise its announcement would stay disabled forever.
	clearGroupAnnouncementBotAbsent(a.bindings.Announcements, chatID)
	scheduleGroupAnnouncementStatusRefresh(a.runtimeOwner.Announcements, chatID)
}

func initializeGroupPrimary(ctx context.Context, initializer approuting.InitializationService, frontendID string, client FeishuClient, chatType, chatID string) (*state.GroupPrimary, error) {
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	chatID = strings.TrimSpace(chatID)
	if chatType != "group" || chatID == "" {
		return nil, nil
	}
	if client == nil {
		return nil, fmt.Errorf("feishu client not initialized")
	}
	result, err := initializer.Ensure(ctx, frontendID, chatType, chatID)
	if err != nil || result == nil {
		return nil, err
	}
	return &state.GroupPrimary{FrontendID: result.FrontendID, ChatID: result.ChatID, ChatType: result.ChatType, Enabled: result.Enabled, LastAssignmentMessageID: result.LastAssignmentMessageID, LastAssignmentCreatedAt: result.LastAssignmentCreatedAt}, nil
}

func lookupGroupPrimary(service approuting.Service, frontendID, chatType, chatID string) *state.GroupPrimary {
	primary, err := service.Lookup(frontendID, chatType, chatID)
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

func groupPrimaryHasState(service approuting.Service, frontendID, chatType, chatID string) bool {
	hasState, err := service.HasState(frontendID, chatType, chatID)
	return err == nil && hasState
}

func groupPrimaryEnabled(service approuting.Service, frontendID, chatType, chatID string) bool {
	enabled, err := service.IsPrimary(frontendID, chatType, chatID)
	return err == nil && enabled
}

func currentBotOpenID(client FeishuClient) string {
	if client == nil {
		return ""
	}
	provider, ok := client.(botOpenIDProvider)
	if !ok {
		return ""
	}
	return strings.TrimSpace(provider.BotOpenID())
}

func currentLiveBotOpenID(client FeishuClient) string {
	if client == nil {
		return ""
	}
	if provider, ok := client.(liveBotOpenIDProvider); ok {
		if openID := strings.TrimSpace(provider.RefreshBotOpenID()); openID != "" {
			return openID
		}
	}
	return currentBotOpenID(client)
}

func LiveBotOpenID(client FeishuClient) func() string {
	return func() string { return currentLiveBotOpenID(client) }
}

func BotDisplayName(client FeishuClient) func() string {
	return func() string { return currentBotDisplayName(client) }
}

func EnsureGroupPrimary(ctx context.Context, initializer approuting.InitializationService, frontendID string, client FeishuClient, chatType, chatID string) error {
	_, err := initializeGroupPrimary(ctx, initializer, frontendID, client, chatType, chatID)
	return err
}

func currentBotName(client FeishuClient) string {
	if client == nil {
		return ""
	}
	provider, ok := client.(botNameProvider)
	if !ok {
		return ""
	}
	return strings.TrimSpace(provider.BotName())
}

func currentBotDisplayName(client FeishuClient) string {
	if name := currentBotName(client); name != "" {
		return name
	}
	return currentBotOpenID(client)
}

func writeGroupPrimaryState(service approuting.Service, frontendID, chatType, chatID string, enabled bool, assignment *feishu.InboundMessage) (*state.GroupPrimary, error) {
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	chatID = strings.TrimSpace(chatID)
	if chatType != "group" || chatID == "" {
		return nil, fmt.Errorf("group chat is required")
	}
	input := approuting.ChangePrimary{
		Frontend: identity.FrontendID(frontendID),
		Chat:     identity.ChatRef{Type: identity.ChatType(chatType), ID: chatID},
		Enabled:  enabled,
	}
	if assignment != nil {
		input.Assignment = &domainrouting.AssignmentStamp{MessageID: assignment.MessageID, CreatedAt: assignment.CreatedAt}
	}
	if _, err := service.SetPrimary(input); err != nil {
		return nil, err
	}
	updated := lookupGroupPrimary(service, frontendID, chatType, chatID)
	if updated == nil {
		return nil, fmt.Errorf("group primary state for %s/%s not found after update", chatType, chatID)
	}
	slog.Info("group primary state written",
		"frontend_id", strings.TrimSpace(frontendID),
		"chat_id", chatID,
		"primary_enabled", updated.Enabled,
		"assignment_message_id", strings.TrimSpace(updated.LastAssignmentMessageID),
	)
	return updated, nil
}

func syncGroupPrimaryAssignment(primary approuting.Service, frontendID string, client FeishuClient, msg *feishu.InboundMessage) (bool, error) {
	assignment, ok := groupPrimaryAssignmentFromMessage(msg)
	if !ok {
		return false, nil
	}
	selfOpenID := currentLiveBotOpenID(client)
	if selfOpenID == "" {
		slog.Warn("group primary assignment skipped without live bot identity",
			"frontend_id", strings.TrimSpace(frontendID),
			"chat_id", msg.ChatID,
			"message_id", msg.MessageID,
		)
		return true, nil
	}
	stale, err := primary.IsStaleAssignment(
		frontendID, msg.ChatType, msg.ChatID, domainrouting.AssignmentStamp{MessageID: msg.MessageID, CreatedAt: msg.CreatedAt},
	)
	if err != nil {
		return true, err
	}
	if stale {
		return true, nil
	}
	enabled := selfOpenID == assignment.TargetBotOpenID
	if enabled {
		// Let the target frontend continue through the normal command handler so
		// it can acknowledge the assignment.
		return false, nil
	}
	_, err = primary.SetPrimary(approuting.ChangePrimary{
		Frontend: identity.FrontendID(frontendID),
		Chat:     identity.ChatRef{Type: identity.ChatType(msg.ChatType), ID: msg.ChatID},
		Enabled:  false,
		Assignment: &domainrouting.AssignmentStamp{
			MessageID: msg.MessageID,
			CreatedAt: msg.CreatedAt,
		},
	})
	if err != nil {
		return true, err
	}
	updated, _ := primary.Lookup(frontendID, msg.ChatType, msg.ChatID)
	if updated == nil {
		return true, fmt.Errorf("group primary state for %s/%s not found after update", msg.ChatType, msg.ChatID)
	}
	slog.Info("group primary state written",
		"frontend_id", strings.TrimSpace(frontendID),
		"chat_id", msg.ChatID,
		"primary_enabled", updated.Enabled,
		"assignment_message_id", strings.TrimSpace(updated.LastAssignmentMessageID),
	)
	return true, nil
}

func isGroupPrimaryControlMessage(msg *feishu.InboundMessage) bool {
	if msg == nil || strings.TrimSpace(msg.ChatType) != "group" {
		return false
	}
	return domainrouting.ParsePrimaryOnCommand(msg.Text) || domainrouting.ParseEmptyBotMention(msg.Text)
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
