package app

import (
	"testing"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCompleteMenuInterruptRejectsStaleTurnCard(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	a := &App{store: store}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-1",
		ActiveThreadID: "thread-new",
		ActiveTurnID:   "turn-new",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := a.completeMenuInterrupt(nil, "sess-1", "turn-old")
	if err != nil {
		t.Fatalf("completeMenuInterrupt: %v", err)
	}
	if resp == nil || resp.Toast == nil {
		t.Fatal("expected warning toast")
	}
	if resp.Toast.Type != "warning" {
		t.Fatalf("unexpected toast type: %q", resp.Toast.Type)
	}
}

func TestCompleteMenuNewRejectsRunningTurn(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	a := &App{store: store}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-1",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := a.completeMenuNew(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1")
	if err != nil {
		t.Fatalf("completeMenuNew: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("expected warning toast, got %#v", resp)
	}
}

func TestCompleteThreadResumeRejectsRunningTurn(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	a := &App{store: store, cfg: config.Default()}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-1",
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := a.completeThreadResume(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1", "thread-2")
	if err != nil {
		t.Fatalf("completeThreadResume: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("expected warning toast, got %#v", resp)
	}
}

func TestCompleteWorkspaceUsePreservesRunningTurnLineage(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	cfg := config.Default()
	cfg.Workspaces = append(cfg.Workspaces, config.Workspace{ID: "alt", Cwd: t.TempDir()})
	a := &App{store: store, cfg: cfg}
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-1",
		WorkspaceID:             "default",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "default",
		ActiveTurnID:            "turn-1",
		ActiveSubmissionID:      "sub-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := a.completeWorkspaceUse(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1", "alt")
	if err != nil {
		t.Fatalf("completeWorkspaceUse: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("expected success toast, got %#v", resp)
	}
	sess := a.store.GetSession("sess-1")
	if sess == nil {
		t.Fatal("expected session to exist")
	}
	if sess.WorkspaceID != "alt" {
		t.Fatalf("workspace = %q, want alt", sess.WorkspaceID)
	}
	if sess.ActiveThreadID != "thread-1" || sess.ActiveThreadWorkspaceID != "default" {
		t.Fatalf("expected thread lineage preserved, got %#v", sess)
	}
	if sess.ActiveTurnID != "turn-1" || sess.ActiveSubmissionID != "sub-1" {
		t.Fatalf("expected turn lineage preserved, got %#v", sess)
	}
}

func TestParseTurnItemToggleName(t *testing.T) {
	requestID, expanded, ok := parseTurnItemToggleName("turn.item.toggle:req-123:expanded")
	if !ok {
		t.Fatal("expected toggle name to parse")
	}
	if requestID != "req-123" {
		t.Fatalf("unexpected request id: %q", requestID)
	}
	if !expanded {
		t.Fatal("expected expanded=true")
	}
}
