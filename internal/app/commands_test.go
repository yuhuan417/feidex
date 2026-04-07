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

func TestHandleCommandStopAliasesInterrupt(t *testing.T) {
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
	if len(sess.Queue) != 1 || sess.Queue[0] != "sub-queued" {
		t.Fatalf("expected /stop to avoid queue mutation, got %#v", sess.Queue)
	}
}
