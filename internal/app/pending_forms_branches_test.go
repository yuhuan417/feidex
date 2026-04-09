package app

import (
	"context"
	"testing"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestPendingFormCancelAndAppendBranches(t *testing.T) {
	a, ff, fc := newTestApp(t)
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	formKinds := []string{"turn_append", "tool_request_user_input_form", "mcp_elicitation_form", "workspace_new"}
	for _, kind := range formKinds {
		if err := a.store.UpsertPending(&state.PendingRequest{
			ID:          kind,
			Kind:        kind,
			SessionKey:  "sess-1",
			OwnerUserID: "user-1",
			RequestIDRaw: `"req-1"`,
			FeishuMsgID: "card-" + kind,
			Status:      "pending",
			PayloadJSON: mustJSON(toolUserInputPayload{ThreadID: "thread-1", TurnID: "turn-1"}),
		}); err != nil {
			t.Fatalf("UpsertPending(%s) error = %v", kind, err)
		}
		resp, err := a.completePendingFormCancel(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": kind}})
		if err != nil || resp == nil || resp.Toast == nil {
			t.Fatalf("completePendingFormCancel(%s) = %#v, %v", kind, resp, err)
		}
	}
	if len(fc.replies) == 0 && len(fc.replyErrors) == 0 {
		t.Fatal("expected form cancel to reply to codex")
	}
	if len(ff.patchedCards) == 0 {
		t.Fatal("expected turn_append cancel path to patch cards")
	}

	pending := &state.PendingRequest{ID: "append-stale", Kind: "turn_append", SessionKey: "sess-1", TurnID: "turn-old", FeishuMsgID: "card-append"}
	if err := a.store.UpsertPending(pending); err != nil {
		t.Fatalf("UpsertPending(append-stale) error = %v", err)
	}
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatType: "group", Text: "hello"}
	if err := a.completeTurnAppendText(msg, pending); err == nil {
		t.Fatal("expected stale turn append to fail")
	}

	pending = &state.PendingRequest{ID: "append-ok", Kind: "turn_append", SessionKey: "sess-1", TurnID: "turn-1", FeishuMsgID: "card-ok"}
	if err := a.store.UpsertPending(pending); err != nil {
		t.Fatalf("UpsertPending(append-ok) error = %v", err)
	}
	fc.callHook = func(_ context.Context, method string, _ any, _ any) error {
		if method == "turn/steer" {
			return nil
		}
		return nil
	}
	if err := a.completeTurnAppendText(msg, pending); err != nil {
		t.Fatalf("completeTurnAppendText(success) error = %v", err)
	}
}
