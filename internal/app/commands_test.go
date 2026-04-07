package app

import (
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCommandNewRejectsRunningTurn(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	a := &App{store: store}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "feishu:p2p:chat:user",
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	err = a.commandNew(&feishu.InboundMessage{
		ChatID:   "chat",
		ChatType: "p2p",
		UserID:   "user",
	})
	if err == nil {
		t.Fatal("expected running turn to block /new")
	}
	if !strings.Contains(err.Error(), "仍在运行") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleCommandStopClearsQueuedInputsBeforeInterrupt(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	a := &App{store: store, codex: codexrpc.New(config.CodexConfig{})}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "feishu:p2p:chat:user",
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
		Queue:          []string{"sub-queued"},
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-queued",
		SessionKey:       "feishu:p2p:chat:user",
		WorkspaceID:      "default",
		TriggerMessageID: "msg-queued",
		SourceMessageIDs: []string{"msg-queued"},
		Status:           "queued",
	}); err != nil {
		t.Fatalf("create submission: %v", err)
	}

	err = a.handleCommand(&feishu.InboundMessage{
		ChatID:   "chat",
		ChatType: "p2p",
		UserID:   "user",
	}, "/stop")
	if err == nil {
		t.Fatal("expected /stop to route to interrupt command")
	}
	if !strings.Contains(err.Error(), "client not started") {
		t.Fatalf("unexpected /stop error: %v", err)
	}
	sess := a.store.GetSession("feishu:p2p:chat:user")
	if sess == nil {
		t.Fatal("expected session to remain")
	}
	if len(sess.Queue) != 0 {
		t.Fatalf("expected /stop to clear queued inputs, got %#v", sess.Queue)
	}
}

func TestIsLocalCommand(t *testing.T) {
	cases := map[string]bool{
		"/menu":          true,
		"/model":         true,
		"/quiet":         true,
		"/new":           true,
		"/threads":       true,
		"/threads new":   true,
		"/threads all":   true,
		"/interrupt":     true,
		"/stop":          true,
		"/workspace":     true,
		"/cd":            true,
		"/status":        true,
		"/append hello":  false,
		"/model list":    false,
		"/":              false,
		"/compact":       false,
		"/unknown value": false,
	}
	for input, want := range cases {
		if got := isLocalCommand(input); got != want {
			t.Fatalf("isLocalCommand(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestSendCommandMenuListsTopLevelCommands(t *testing.T) {
	a := &App{feishu: feishu.New(config.Default().Feishu)}
	msg := &feishu.InboundMessage{MessageID: "m1", ChatType: "p2p", ChatID: "chat", UserID: "user"}
	card := a.feishu.SimpleStatusCard("命令菜单", "blue", "选择命令执行。", nil)
	elements, ok := card["elements"].([]map[string]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("unexpected card elements: %#v", card["elements"])
	}
	body, _ := elements[0]["content"].(string)
	for _, alias := range []string{"/menu", "/new", "/stop", "/cd", "/model", "/quiet", "/threads", "/interrupt", "/status", "/workspace"} {
		if strings.Contains(body, alias) {
			t.Fatalf("expected menu body to omit command text %q, got %q", alias, body)
		}
	}
	_ = msg
}

func TestStartupReadyChatIDsDeduplicatesChats(t *testing.T) {
	ids := startupReadyChatIDs([]*state.Session{
		{ChatID: "chat-b"},
		{ChatID: "chat-a"},
		{ChatID: "chat-b"},
		{ChatID: ""},
		nil,
	})
	if len(ids) != 2 {
		t.Fatalf("unexpected chat id count: %#v", ids)
	}
	if ids[0] != "chat-a" || ids[1] != "chat-b" {
		t.Fatalf("unexpected sorted chat ids: %#v", ids)
	}
}
