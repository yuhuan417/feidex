package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestFileApprovalHydratesFromStartedItem(t *testing.T) {
	a, _, _ := newTestApp(t)
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	changedPath := filepath.Join(a.cfg.Workspaces[0].Cwd, "dir", "main.go")
	a.handleNotification("item/started", json.RawMessage(fmt.Sprintf(`{"threadId":"thread-1","turnId":"turn-1","item":{"id":"item-2","type":"fileChange","status":"inProgress","changes":[{"path":%q,"kind":"update"}]}}`, changedPath)))
	a.handleServerRequest(codexrpc.RequestEnvelope{
		ID:     json.RawMessage(`"file-1"`),
		Method: "item/fileChange/requestApproval",
		Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-2","reason":"need review"}`),
	})
	pending := a.store.PendingByID("file-1")
	if pending == nil {
		t.Fatal("expected file approval pending request")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal pending payload: %v", err)
	}
	request, _ := payload["request"].(map[string]any)
	entries := collectFileApprovalEntriesWithWorkspace(request, a.cfg.Workspaces[0].Cwd)
	if len(entries) != 1 {
		t.Fatalf("approval entries = %+v, want 1 hydrated entry", entries)
	}
	if entries[0].Path != "dir/main.go" || entries[0].Kind != "update" {
		t.Fatalf("approval entry = %+v, want relative hydrated file change", entries[0])
	}
}

func TestApprovalActionWaitsForServerRequestResolved(t *testing.T) {
	a, _, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.onCommandApproval(codexrpc.RequestEnvelope{
		ID:     json.RawMessage(`"cmd-1"`),
		Method: "item/commandExecution/requestApproval",
		Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","command":"pwd"}`),
	})
	if pending := a.store.PendingByID("cmd-1"); pending == nil || pending.Status != "pending" {
		t.Fatalf("pending after approval request = %+v", pending)
	}
	if _, err := a.completeApprovalAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "cmd-1"},
	}, "approval.command.accept"); err != nil {
		t.Fatalf("completeApprovalAction() error = %v", err)
	}
	if pending := a.store.PendingByID("cmd-1"); pending == nil || pending.Status != "replied" {
		t.Fatalf("pending after user reply = %+v, want replied", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "waiting_approval" {
		t.Fatalf("submission after user reply = %+v, want waiting_approval", updated)
	}
	a.handleNotification("serverRequest/resolved", json.RawMessage(`{"threadId":"thread-1","requestId":"cmd-1"}`))
	if pending := a.store.PendingByID("cmd-1"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after serverRequest/resolved = %+v, want resolved", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "running" {
		t.Fatalf("submission after serverRequest/resolved = %+v, want running", updated)
	}
}

func TestServerRequestResolvedWaitsForOtherOpenRequests(t *testing.T) {
	a, _, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	a.onCommandApproval(codexrpc.RequestEnvelope{
		ID:     json.RawMessage(`"cmd-1"`),
		Method: "item/commandExecution/requestApproval",
		Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","command":"pwd"}`),
	})
	a.onFileApproval(codexrpc.RequestEnvelope{
		ID:     json.RawMessage(`"file-1"`),
		Method: "item/fileChange/requestApproval",
		Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-2","files":["README.md"]}`),
	})
	if err := a.store.UpdatePending("cmd-1", func(req *state.PendingRequest) { req.Status = "replied" }); err != nil {
		t.Fatalf("UpdatePending(cmd-1) error = %v", err)
	}
	if err := a.store.UpdatePending("file-1", func(req *state.PendingRequest) { req.Status = "replied" }); err != nil {
		t.Fatalf("UpdatePending(file-1) error = %v", err)
	}
	a.handleNotification("serverRequest/resolved", json.RawMessage(`{"threadId":"thread-1","requestId":"cmd-1"}`))
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "waiting_approval" {
		t.Fatalf("submission after first resolved = %+v, want waiting_approval", updated)
	}
	a.handleNotification("serverRequest/resolved", json.RawMessage(`{"threadId":"thread-1","requestId":"file-1"}`))
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "running" {
		t.Fatalf("submission after all resolved = %+v, want running", updated)
	}
}
