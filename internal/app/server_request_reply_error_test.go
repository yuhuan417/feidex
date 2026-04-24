package app

import (
	"encoding/json"
	"errors"
	"testing"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCompleteUserInputAnswerKeepsPendingWhenCodexReplyFails(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-1"
	sub := seedActiveSubmission(t, a, sessionKey, "thread-1", "turn-1")
	newOutboundCardService(a).sendUserInputCard(json.RawMessage(`"input-1"`), toolUserInputPayload{
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		Questions: []toolUserInputQuestion{
			{ID: "mode", Question: "Pick one", Options: []toolUserInputOption{{Label: "Fast"}, {Label: "Safe"}}},
		},
	})
	fc.replyErr = errors.New("write failed")

	resp, err := completeUserInputAnswer(a,&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "input-1", "question_id": "mode", "answer": "Fast"},
	})
	if err != nil {
		t.Fatalf("completeUserInputAnswer() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeUserInputAnswer() = %#v, want warning toast", resp)
	}
	if pending := a.store.PendingByID("input-1"); pending == nil || pending.Status != "pending" {
		t.Fatalf("pending after failed user input reply = %+v, want pending", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "waiting_user_input" {
		t.Fatalf("submission after failed user input reply = %+v, want waiting_user_input", updated)
	}
}

func TestCompleteUserInputFormAnswerKeepsPendingWhenCodexReplyFails(t *testing.T) {
	a, _, fc := newTestApp(t)
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:           "input-form-1",
		RequestIDRaw: `"input-form-1"`,
		Kind:         "tool_request_user_input_form",
		SessionKey:   "sess-1",
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		OwnerUserID:  "user-1",
		PayloadJSON: mustJSON(toolUserInputPayload{
			Questions: []toolUserInputQuestion{
				{ID: "mode", Question: "Choose mode", Options: []toolUserInputOption{{Label: "Fast"}, {Label: "Safe"}}},
			},
		}),
		Status: "pending",
	}); err != nil {
		t.Fatalf("UpsertPending(input-form-1) error = %v", err)
	}
	fc.replyErr = errors.New("write failed")

	resp, err := completeUserInputAnswer(a,&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "input-form-1"},
		FormValue:   map[string]any{"mode": "Fast"},
	})
	if err != nil {
		t.Fatalf("completeUserInputAnswer(form) error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeUserInputAnswer(form) = %#v, want warning toast", resp)
	}
	if pending := a.store.PendingByID("input-form-1"); pending == nil || pending.Status != "pending" {
		t.Fatalf("pending after failed user input form reply = %+v, want pending", pending)
	}
}

func TestCompleteToolUserInputTextKeepsPendingWhenCodexReplyFails(t *testing.T) {
	a, _, fc := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	_ = appState(a).setSubmissionStatus(sub.ID, "waiting_user_input")
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:           "input-text-1",
		RequestIDRaw: `"input-text-1"`,
		Backend:      backendCodex,
		Kind:         "tool_request_user_input_form",
		SessionKey:   "sess-1",
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		OwnerUserID:  "user-1",
		PayloadJSON: mustJSON(toolUserInputPayload{
			Questions: []toolUserInputQuestion{
				{ID: "mode", Question: "Choose mode", Options: []toolUserInputOption{{Label: "Fast"}, {Label: "Safe"}}},
			},
		}),
		Status: "pending",
	}); err != nil {
		t.Fatalf("UpsertPending(input-text-1) error = %v", err)
	}
	fc.replyErr = errors.New("write failed")

	err := newPendingInputService(a).completeToolUserInputText(&feishu.InboundMessage{Text: "Fast"}, a.store.PendingByID("input-text-1"))
	if err == nil || err.Error() != "write failed" {
		t.Fatalf("completeToolUserInputText() error = %v, want write failed", err)
	}
	if pending := a.store.PendingByID("input-text-1"); pending == nil || pending.Status != "pending" {
		t.Fatalf("pending after failed user input text reply = %+v, want pending", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "waiting_user_input" {
		t.Fatalf("submission after failed user input text reply = %+v, want waiting_user_input", updated)
	}
}

func TestCompleteElicitationURLActionKeepsPendingWhenCodexReplyFails(t *testing.T) {
	a, _, fc := newTestApp(t)
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:           "url-1",
		RequestIDRaw: `"url-1"`,
		Kind:         "mcp_elicitation_url",
		SessionKey:   "sess-1",
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		OwnerUserID:  "user-1",
		Status:       "pending",
	}); err != nil {
		t.Fatalf("UpsertPending(url) error = %v", err)
	}
	fc.replyErr = errors.New("write failed")

	resp, err := completeElicitationURLAction(a,&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "url-1"},
	}, "elicitation_url.accept")
	if err != nil {
		t.Fatalf("completeElicitationURLAction() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completeElicitationURLAction() = %#v, want warning toast", resp)
	}
	if pending := a.store.PendingByID("url-1"); pending == nil || pending.Status != "pending" {
		t.Fatalf("pending after failed elicitation url reply = %+v, want pending", pending)
	}
}

func TestCompletePendingFormCancelKeepsPendingWhenCodexReplyFails(t *testing.T) {
	a, _, fc := newTestApp(t)
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:           "form-1",
		RequestIDRaw: `"form-1"`,
		Kind:         "mcp_elicitation_form",
		SessionKey:   "sess-1",
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		OwnerUserID:  "user-1",
		Status:       "pending",
	}); err != nil {
		t.Fatalf("UpsertPending(form) error = %v", err)
	}
	fc.replyErr = errors.New("write failed")

	resp, err := a.completePendingFormCancel(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "form-1"},
	})
	if err != nil {
		t.Fatalf("completePendingFormCancel() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("completePendingFormCancel() = %#v, want warning toast", resp)
	}
	if pending := a.store.PendingByID("form-1"); pending == nil || pending.Status != "pending" {
		t.Fatalf("pending after failed form cancel reply = %+v, want pending", pending)
	}
}

func TestCompleteElicitationFormTextKeepsPendingWhenCodexReplyFails(t *testing.T) {
	a, _, fc := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	_ = appState(a).setSubmissionStatus(sub.ID, "waiting_user_input")
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:           "elicit-form-1",
		RequestIDRaw: `"elicit-form-1"`,
		Backend:      backendCodex,
		Kind:         "mcp_elicitation_form",
		SessionKey:   "sess-1",
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		OwnerUserID:  "user-1",
		PayloadJSON: mustJSON(elicitationFormPayload{
			Schema: map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}},
		}),
		Status: "pending",
	}); err != nil {
		t.Fatalf("UpsertPending(elicit-form-1) error = %v", err)
	}
	fc.replyErr = errors.New("write failed")

	err := newPendingInputService(a).completeElicitationFormText(&feishu.InboundMessage{Text: "Feidex"}, a.store.PendingByID("elicit-form-1"))
	if err == nil || err.Error() != "write failed" {
		t.Fatalf("completeElicitationFormText() error = %v, want write failed", err)
	}
	if pending := a.store.PendingByID("elicit-form-1"); pending == nil || pending.Status != "pending" {
		t.Fatalf("pending after failed elicitation form reply = %+v, want pending", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "waiting_user_input" {
		t.Fatalf("submission after failed elicitation form reply = %+v, want waiting_user_input", updated)
	}
}
