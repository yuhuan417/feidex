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

type botOpenIDProvider interface {
	BotOpenID() string
}

type liveBotOpenIDProvider interface {
	RefreshBotOpenID() string
}

type botNameProvider interface {
	BotName() string
}

type groupPrimaryAssignment struct {
	TargetBotOpenID string
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
	return a.State().EnsureGroupPrimary(&state.GroupPrimary{
		FrontendID: a.FrontendID(),
		ChatID:     chatID,
		ChatType:   chatType,
		Enabled:    enabled,
	})
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
	record := groupPrimaryForChat(a, chatType, chatID)
	if record != nil && staleGroupPrimaryAssignment(record, assignment) {
		return record, nil
	}
	if record == nil {
		record = &state.GroupPrimary{
			ID:         appstate.DefaultGroupPrimaryID(a.FrontendID(), chatType, chatID),
			FrontendID: a.FrontendID(),
			ChatID:     chatID,
			ChatType:   chatType,
		}
	}
	record.Enabled = enabled
	if assignment != nil {
		record.LastAssignmentMessageID = strings.TrimSpace(assignment.MessageID)
		record.LastAssignmentCreatedAt = assignment.CreatedAt
	}
	if err := a.State().SaveGroupPrimary(record); err != nil {
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
	return parsePrimaryOnCommandFromText(msg.Text) || parseEmptyBotMentionFromText(msg.Text)
}

func staleGroupPrimaryAssignment(record *state.GroupPrimary, assignment *feishu.InboundMessage) bool {
	if record == nil || assignment == nil {
		return false
	}
	messageID := strings.TrimSpace(assignment.MessageID)
	if messageID != "" && messageID == strings.TrimSpace(record.LastAssignmentMessageID) {
		return true
	}
	if assignment.CreatedAt == 0 || record.LastAssignmentCreatedAt == 0 {
		return false
	}
	return assignment.CreatedAt < record.LastAssignmentCreatedAt
}

func groupPrimaryAssignmentFromMessage(msg *feishu.InboundMessage) (groupPrimaryAssignment, bool) {
	if msg == nil || strings.TrimSpace(msg.ChatType) != "group" {
		return groupPrimaryAssignment{}, false
	}
	return groupPrimaryAssignmentFromTextAndMentions(msg.Text, msg.MentionedOpenIDs)
}

func groupPrimaryAssignmentFromTextAndMentions(text string, mentionedOpenIDs []string) (groupPrimaryAssignment, bool) {
	if !hasExactlyOneMentionedOpenID(mentionedOpenIDs) {
		return groupPrimaryAssignment{}, false
	}
	targetBotOpenID := singleMentionedOpenID(mentionedOpenIDs)
	if parsePrimaryOnCommandFromText(text) || parseEmptyBotMentionFromText(text) {
		return groupPrimaryAssignment{TargetBotOpenID: targetBotOpenID}, true
	}
	return groupPrimaryAssignment{}, false
}

func groupPrimaryAssignmentForCommand(msg *feishu.InboundMessage) (groupPrimaryAssignment, bool) {
	if msg == nil || strings.TrimSpace(msg.ChatType) != "group" {
		return groupPrimaryAssignment{}, false
	}
	return groupPrimaryAssignmentFromTextAndMentions("/primary on", msg.MentionedOpenIDs)
}

func hasExactlyOneMentionedOpenID(mentionedOpenIDs []string) bool {
	return singleMentionedOpenID(mentionedOpenIDs) != "" && countMentionedOpenIDs(mentionedOpenIDs) == 1
}

func countMentionedOpenIDs(mentionedOpenIDs []string) int {
	count := 0
	for _, openID := range mentionedOpenIDs {
		if strings.TrimSpace(openID) != "" {
			count++
		}
	}
	return count
}

func singleMentionedOpenID(mentionedOpenIDs []string) string {
	var target string
	for _, openID := range mentionedOpenIDs {
		value := strings.TrimSpace(openID)
		if value == "" {
			continue
		}
		if target != "" {
			return ""
		}
		target = value
	}
	return target
}

func parsePrimaryOnCommandFromText(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 2 {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(fields[len(fields)-1]), "on") {
		return false
	}
	prefix := fields[:len(fields)-2]
	switch strings.TrimSpace(fields[len(fields)-2]) {
	case "/primary":
	case "primary":
		if len(prefix) == 0 || strings.TrimSpace(prefix[len(prefix)-1]) != "/workspace" {
			return false
		}
		prefix = prefix[:len(prefix)-1]
	default:
		return false
	}
	for _, field := range prefix {
		if !strings.HasPrefix(strings.TrimSpace(field), "@") {
			return false
		}
	}
	return true
}

func parseEmptyBotMentionFromText(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	for _, field := range fields {
		if !strings.HasPrefix(strings.TrimSpace(field), "@") {
			return false
		}
	}
	return true
}
