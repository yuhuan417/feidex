package app

import (
	"testing"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestPendingFormCancelBranches(t *testing.T) {
	a, ff, fc := newTestApp(t)
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	formKinds := []string{"tool_request_user_input_form", "mcp_elicitation_form", "workspace_new"}
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
	if len(ff.patchedCards) != 0 {
		t.Fatalf("unexpected patched cards on cancel branches: %+v", ff.patchedCards)
	}
}
