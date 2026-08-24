package app

import (
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestGroupMessagePolicyRoutesPrimaryMentionsAndReplies(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	cfg := config.Default()
	cfg.Feishu.GroupAtOnly = true
	a := &App{
		cfg:        cfg,
		store:      store,
		frontendID: "frontend-a",
	}
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:       "binding-primary",
		ChatID:   "chat-1",
		ChatType: "group",
		Status:   state.AgentBindingStatusActive.String(),
		Primary:  true,
	}); err != nil {
		t.Fatalf("SaveAgentBinding(primary) error = %v", err)
	}

	if !shouldAcceptGroupMessage(a, "chat-1", "", "", false, false, false) {
		t.Fatal("primary binding rejected an unmentioned group message")
	}
	defaultedTopLevel := &feishu.InboundMessage{MessageID: "top-1", RootMessageID: "top-1", ChatType: "group", ChatID: "chat-1"}
	if got := groupPolicyRootMessageID(defaultedTopLevel); got != "" {
		t.Fatalf("groupPolicyRootMessageID(defaulted top-level) = %q, want empty", got)
	}
	if !shouldAcceptGroupMessage(a, "chat-1", groupPolicyRootMessageID(defaultedTopLevel), defaultedTopLevel.ParentMessageID, false, false, false) {
		t.Fatal("primary binding rejected an unmentioned top-level group message with defaulted root")
	}
	if !shouldAcceptGroupMessage(a, "chat-1", "", "", true, true, false) {
		t.Fatal("primary binding rejected a direct self mention")
	}
	if shouldAcceptGroupMessage(a, "chat-1", "", "", false, true, false) {
		t.Fatal("primary binding accepted a mention of another bot")
	}

	if err := store.UpsertMessageLink(&state.MessageLink{
		FrontendID: "frontend-a",
		MessageID:  "bot-reply-1",
		SessionKey: "feishu:frontend:frontend-a:group:chat-1:root:root-1",
	}); err != nil {
		t.Fatalf("UpsertMessageLink() error = %v", err)
	}
	if !shouldAcceptGroupMessage(a, "chat-1", "bot-reply-1", "user-reply-1", false, false, false) {
		t.Fatal("primary binding rejected a reply to its own message")
	}
	reply := &feishu.InboundMessage{MessageID: "reply-1", RootMessageID: "bot-reply-1", ParentMessageID: "bot-reply-1", ChatType: "group", ChatID: "chat-1"}
	if got := groupPolicyRootMessageID(reply); got != "bot-reply-1" {
		t.Fatalf("groupPolicyRootMessageID(reply) = %q, want bot-reply-1", got)
	}
	if shouldAcceptGroupMessage(a, "chat-1", "other-bot-reply", "user-reply-2", false, false, false) {
		t.Fatal("primary binding accepted a reply without a local message link")
	}
	if err := store.UpsertMessageLink(&state.MessageLink{
		FrontendID: "frontend-a",
		MessageID:  "bot-parent-1",
		SessionKey: "feishu:frontend:frontend-a:group:chat-1:root:root-1",
	}); err != nil {
		t.Fatalf("UpsertMessageLink(parent) error = %v", err)
	}
	if !shouldAcceptGroupMessage(a, "chat-1", "original-root-1", "bot-parent-1", false, false, false) {
		t.Fatal("primary binding rejected a reply with only a parent message link")
	}

	cfg.Feishu.RespondToAtEveryone = true
	if !shouldAcceptGroupMessage(a, "chat-1", "", "", false, false, true) {
		t.Fatal("primary binding rejected configured @everyone")
	}

	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:       "binding-pending",
		ChatID:   "chat-2",
		ChatType: "group",
		Status:   state.AgentBindingStatusPending.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding(pending) error = %v", err)
	}
	if !shouldAcceptGroupMessage(a, "chat-2", "", "", true, true, false) {
		t.Fatal("pending binding rejected direct onboarding mention")
	}
	if shouldAcceptGroupMessage(a, "chat-2", "", "", false, false, false) {
		t.Fatal("pending binding accepted an unmentioned message")
	}
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:       "binding-pending-primary",
		ChatID:   "chat-3",
		ChatType: "group",
		Status:   state.AgentBindingStatusPending.String(),
		Primary:  true,
	}); err != nil {
		t.Fatalf("SaveAgentBinding(pending primary) error = %v", err)
	}
	if !shouldAcceptGroupMessage(a, "chat-3", "", "", false, false, false) {
		t.Fatal("pending primary binding rejected an unmentioned message")
	}
}

func TestGroupMessagePolicyKeepsNonPrimaryRepliesLocal(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	cfg := config.Default()
	a := &App{cfg: cfg, store: store, frontendID: "frontend-b"}
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:       "binding-client",
		ChatID:   "chat-1",
		ChatType: "group",
		Status:   state.AgentBindingStatusActive.String(),
		Primary:  false,
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}
	if shouldAcceptGroupMessage(a, "chat-1", "", "", false, false, false) {
		t.Fatal("non-primary binding accepted an unmentioned group message")
	}
	if !shouldAcceptGroupMessage(a, "chat-1", "", "", true, true, false) {
		t.Fatal("non-primary binding rejected direct mention")
	}
	if err := store.UpsertMessageLink(&state.MessageLink{
		FrontendID: "frontend-b",
		MessageID:  "client-reply",
		SessionKey: "feishu:frontend:frontend-b:group:chat-1:root:root-1",
	}); err != nil {
		t.Fatalf("UpsertMessageLink() error = %v", err)
	}
	if !shouldAcceptGroupMessage(a, "chat-1", "client-reply", "user-reply", false, false, false) {
		t.Fatal("non-primary binding rejected reply to its own message")
	}
}

func TestBindingDoesNotChangeRootScopedSessionKey(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	cfg := config.Default()
	a := &App{cfg: cfg, store: store, frontendID: "frontend-a"}
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:       "binding-server",
		ChatID:   "chat-1",
		ChatType: "group",
		Status:   state.AgentBindingStatusPending.String(),
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}
	first := makeSessionKey(a, &feishu.InboundMessage{ChatType: "group", ChatID: "chat-1", MessageID: "m-1"})
	second := makeSessionKey(a, &feishu.InboundMessage{ChatType: "group", ChatID: "chat-1", MessageID: "m-2", RootMessageID: "root-2"})
	firstWant := "feishu:frontend:frontend-a:group:chat-1:root:m-1"
	secondWant := "feishu:frontend:frontend-a:group:chat-1:root:root-2"
	if first != firstWant || second != secondWant || first == second {
		t.Fatalf("binding session keys = %q / %q, want %q / %q", first, second, firstWant, secondWant)
	}
	frontendID, chatType, chatID, rootID, userID := parseSessionKey(first)
	if frontendID != "frontend-a" || chatType != "group" || chatID != "chat-1" || rootID != "m-1" || userID != "" {
		t.Fatalf("parsed root session key = %q %q %q %q %q", frontendID, chatType, chatID, rootID, userID)
	}
}

func TestPrimaryBindingHandlesUnmentionedSlashCommand(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.frontendID = "bot-a"
	if err := a.State().SaveAgentBinding(&state.AgentBinding{
		ID:          "binding-primary-slash",
		FrontendID:  "bot-a",
		ChatID:      "chat-slash",
		ChatType:    "group",
		WorkspaceID: "default",
		Status:      state.AgentBindingStatusActive.String(),
		Primary:     true,
	}); err != nil {
		t.Fatalf("SaveAgentBinding() error = %v", err)
	}

	a.HandleFeishuMessage(&feishu.InboundMessage{
		MessageID:     "slash-1",
		ChatID:        "chat-slash",
		ChatType:      "group",
		UserID:        "user-1",
		Text:          "/menu",
		RootMessageID: "slash-1",
	})

	cards := ff.replyCardsSnapshot()
	if len(cards) != 1 {
		t.Fatalf("reply cards = %d, want 1", len(cards))
	}
	if body := cardMarkdownContent(t, cards[0]); !strings.Contains(body, "当前位置：主菜单") {
		t.Fatalf("/menu reply body = %q, want main menu", body)
	}
}
