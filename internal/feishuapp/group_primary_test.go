package feishuapp

import (
	"context"
	"fmt"
	"testing"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func ensureGroupPrimaryInitialized(ctx context.Context, a *Frontend, chatType, chatID string) (*state.GroupPrimary, error) {
	if a == nil {
		return nil, nil
	}
	return initializeGroupPrimary(ctx, a.bindings.PrimaryInitialization, a.FrontendID(), a.feishu, chatType, chatID)
}

func groupPrimaryForChat(a *Frontend, chatType, chatID string) *state.GroupPrimary {
	if a == nil || a.store == nil {
		return nil
	}
	return lookupGroupPrimary(a.bindings.Primary, a.FrontendID(), chatType, chatID)
}

func isGroupPrimary(a *Frontend, chatType, chatID string) bool {
	return a != nil && a.store != nil && groupPrimaryEnabled(a.bindings.Primary, a.FrontendID(), chatType, chatID)
}

func setGroupPrimaryState(a *Frontend, chatType, chatID string, enabled bool, assignment *feishu.InboundMessage) (*state.GroupPrimary, error) {
	if a == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	return writeGroupPrimaryState(a.bindings.Primary, a.FrontendID(), chatType, chatID, enabled, assignment)
}

func setGroupPrimary(a *Frontend, chatType, chatID string, enabled bool) (*state.GroupPrimary, error) {
	return setGroupPrimaryState(a, chatType, chatID, enabled, nil)
}

func TestGroupPrimaryStateIsIndependentPerFrontend(t *testing.T) {
	a, ffA, _ := newTestApp(t)
	a.frontendID = "bot-a"
	recomposeTestApp(a)
	ffA.botOpenID = "bot-a-open"
	b, ffB, _ := newTestApp(t)
	b.frontendID = "bot-b"
	recomposeTestApp(b)
	b.store = a.store
	recomposeTestApp(b)
	ffB.botOpenID = "bot-b-open"

	if _, err := setGroupPrimary(a, "group", "chat-primary", true); err != nil {
		t.Fatal(err)
	}
	if _, err := setGroupPrimary(b, "group", "chat-primary", false); err != nil {
		t.Fatal(err)
	}
	if !isGroupPrimary(a, "group", "chat-primary") || isGroupPrimary(b, "group", "chat-primary") {
		t.Fatalf("independent primary states = %v/%v", isGroupPrimary(a, "group", "chat-primary"), isGroupPrimary(b, "group", "chat-primary"))
	}
	if got := groupPrimaryForChat(a, "group", "chat-primary"); got == nil || got.FrontendID != "bot-a" || !got.Enabled {
		t.Fatalf("frontend-a state = %+v", got)
	}
	if got := groupPrimaryForChat(b, "group", "chat-primary"); got == nil || got.FrontendID != "bot-b" || got.Enabled {
		t.Fatalf("frontend-b state = %+v", got)
	}
}

func TestGroupPrimaryAssignmentEachFrontendSetsItsOwnState(t *testing.T) {
	a, ffA, _ := newTestApp(t)
	a.frontendID = "bot-a"
	recomposeTestApp(a)
	ffA.botOpenID = "bot-a-open"
	b, ffB, _ := newTestApp(t)
	b.frontendID = "bot-b"
	recomposeTestApp(b)
	b.store = a.store
	recomposeTestApp(b)
	ffB.botOpenID = "bot-b-open"

	if _, err := setGroupPrimary(a, "group", "chat-primary", true); err != nil {
		t.Fatal(err)
	}
	msg := func(id string) *feishu.InboundMessage {
		return &feishu.InboundMessage{
			MessageID:        id,
			ChatID:           "chat-primary",
			ChatType:         "group",
			UserID:           "user-1",
			Text:             "/primary on",
			MentionedOpenIDs: []string{"bot-b-open"},
			RootMessageID:    id,
		}
	}

	a.HandleFeishuMessage(msg("primary-switch-a"))
	if isGroupPrimary(a, "group", "chat-primary") {
		t.Fatal("non-target frontend remained primary")
	}
	if cards := ffA.replyCardsSnapshot(); len(cards) != 0 {
		t.Fatalf("non-target frontend reply cards = %d, want 0", len(cards))
	}

	b.HandleFeishuMessage(msg("primary-switch-b"))
	if !isGroupPrimary(b, "group", "chat-primary") {
		t.Fatal("target frontend did not become primary")
	}
	if cards := ffB.replyCardsSnapshot(); len(cards) != 1 {
		t.Fatalf("target frontend reply cards = %d, want 1", len(cards))
	}
	if got := groupPrimaryForChat(a, "group", "chat-primary"); got == nil || got.Enabled {
		t.Fatalf("frontend-a state after handoff = %+v", got)
	}
	if got := groupPrimaryForChat(b, "group", "chat-primary"); got == nil || !got.Enabled {
		t.Fatalf("frontend-b state after handoff = %+v", got)
	}
}

func TestEmptyBotMentionSetsOnlyTargetFrontendPrimary(t *testing.T) {
	a, ffA, _ := newTestApp(t)
	a.frontendID = "bot-a"
	recomposeTestApp(a)
	ffA.botOpenID = "bot-a-open"
	b, ffB, _ := newTestApp(t)
	b.frontendID = "bot-b"
	recomposeTestApp(b)
	b.store = a.store
	recomposeTestApp(b)
	ffB.botOpenID = "bot-b-open"

	if _, err := setGroupPrimary(a, "group", "chat-empty-primary", true); err != nil {
		t.Fatal(err)
	}
	msg := func(id string) *feishu.InboundMessage {
		return &feishu.InboundMessage{
			MessageID:        id,
			ChatID:           "chat-empty-primary",
			ChatType:         "group",
			UserID:           "user-1",
			Text:             "/primary on",
			MentionedOpenIDs: []string{"bot-b-open"},
			RootMessageID:    id,
		}
	}

	a.HandleFeishuMessage(msg("empty-primary-a"))
	b.HandleFeishuMessage(msg("empty-primary-b"))

	if isGroupPrimary(a, "group", "chat-empty-primary") || !isGroupPrimary(b, "group", "chat-empty-primary") {
		t.Fatal("empty mention did not converge local frontend states")
	}
	if len(ffA.replyCardsSnapshot()) != 0 || len(ffB.replyCardsSnapshot()) != 1 {
		t.Fatal("only the target bot should acknowledge the empty mention")
	}
}

func TestStaleGroupPrimaryAssignmentDoesNotRevertState(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.frontendID = "bot-a"
	recomposeTestApp(a)
	ff.botOpenID = "bot-a-open"

	newer := &feishu.InboundMessage{
		MessageID:        "primary-new",
		ChatID:           "chat-primary-stale",
		ChatType:         "group",
		Text:             "/primary on",
		MentionedOpenIDs: []string{"bot-a-open"},
		CreatedAt:        200,
	}
	older := *newer
	older.MessageID = "primary-old"
	older.CreatedAt = 100

	if _, err := setGroupPrimaryState(a, "group", newer.ChatID, true, newer); err != nil {
		t.Fatal(err)
	}
	if _, err := setGroupPrimaryState(a, "group", older.ChatID, false, &older); err != nil {
		t.Fatal(err)
	}
	if got := groupPrimaryForChat(a, "group", newer.ChatID); got == nil || !got.Enabled || got.LastAssignmentMessageID != newer.MessageID {
		t.Fatalf("stale assignment changed state = %+v", got)
	}
}

func TestGroupPrimaryEnsureInitializesOnlyThisFrontend(t *testing.T) {
	a, ffA, _ := newTestApp(t)
	a.frontendID = "bot-a"
	recomposeTestApp(a)
	ffA.botOpenID = "bot-a-open"
	ffA.groupBotCounts = map[string]int{"chat-primary-init": 1}
	b, ffB, _ := newTestApp(t)
	b.frontendID = "bot-b"
	recomposeTestApp(b)
	b.store = a.store
	recomposeTestApp(b)
	ffB.botOpenID = "bot-b-open"
	ffB.groupBotCounts = map[string]int{"chat-primary-init": 2}

	if _, err := ensureGroupPrimaryInitialized(context.Background(), a, "group", "chat-primary-init"); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureGroupPrimaryInitialized(context.Background(), b, "group", "chat-primary-init"); err != nil {
		t.Fatal(err)
	}
	if got := groupPrimaryForChat(a, "group", "chat-primary-init"); got == nil || !got.Enabled {
		t.Fatalf("frontend-a initialized state = %+v", got)
	}
	if got := groupPrimaryForChat(b, "group", "chat-primary-init"); got == nil || got.Enabled {
		t.Fatalf("frontend-b initialized state = %+v", got)
	}
}

func TestAmbiguousPrimaryAssignmentsDoNotChangeLocalState(t *testing.T) {
	a, ffA, _ := newTestApp(t)
	a.frontendID = "bot-a"
	recomposeTestApp(a)
	ffA.botOpenID = "bot-a-open"
	b, ffB, _ := newTestApp(t)
	b.frontendID = "bot-b"
	recomposeTestApp(b)
	b.store = a.store
	recomposeTestApp(b)
	ffB.botOpenID = "bot-b-open"

	if _, err := setGroupPrimary(a, "group", "chat-primary-ambiguous", true); err != nil {
		t.Fatal(err)
	}
	if _, err := setGroupPrimary(b, "group", "chat-primary-ambiguous", false); err != nil {
		t.Fatal(err)
	}

	messages := []*feishu.InboundMessage{
		{
			MessageID: "primary-bare",
			ChatID:    "chat-primary-ambiguous",
			ChatType:  "group",
			Text:      "/primary on",
		},
		{
			MessageID:        "primary-multi",
			ChatID:           "chat-primary-ambiguous",
			ChatType:         "group",
			Text:             "@bot-a @bot-b /primary on",
			MentionedOpenIDs: []string{"bot-a-open", "bot-b-open"},
		},
		{
			MessageID:        "primary-empty-multi",
			ChatID:           "chat-primary-ambiguous",
			ChatType:         "group",
			Text:             "@bot-a @bot-b",
			MentionedOpenIDs: []string{"bot-a-open", "bot-b-open"},
		},
	}
	for _, msg := range messages {
		a.HandleFeishuMessage(msg)
		b.HandleFeishuMessage(msg)
	}

	if !isGroupPrimary(a, "group", "chat-primary-ambiguous") || isGroupPrimary(b, "group", "chat-primary-ambiguous") {
		t.Fatalf("ambiguous assignments changed local state: a=%v b=%v",
			isGroupPrimary(a, "group", "chat-primary-ambiguous"),
			isGroupPrimary(b, "group", "chat-primary-ambiguous"))
	}
	if got := len(ffA.replyCardsSnapshot()); got != 0 {
		t.Fatalf("frontend-a replied to ambiguous primary assignment: %d", got)
	}
	if got := len(ffB.replyCardsSnapshot()); got != 0 {
		t.Fatalf("frontend-b replied to ambiguous primary assignment: %d", got)
	}
}
