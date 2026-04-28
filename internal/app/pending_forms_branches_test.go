package app

import (
	"strings"
	"testing"

	"feidex/internal/app/pendingforms"
	appreview "feidex/internal/app/review"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestPendingFormCancelBranches(t *testing.T) {
	a, ff, fc := newTestApp(t)
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	formKinds := []string{"tool_request_user_input_form", "mcp_elicitation_form", "workspace_new", "workspace_clone"}
	for _, kind := range formKinds {
		if err := a.store.UpsertPending(&state.PendingRequest{
			ID:           kind,
			Kind:         kind,
			SessionKey:   "sess-1",
			OwnerUserID:  "user-1",
			RequestIDRaw: `"req-1"`,
			FeishuMsgID:  "card-" + kind,
			Status:       "pending",
			PayloadJSON:  mustJSON(pendingforms.ToolUserInputPayload{ThreadID: "thread-1", TurnID: "turn-1"}),
		}); err != nil {
			t.Fatalf("UpsertPending(%s) error = %v", kind, err)
		}
		resp, err := completePendingFormCancelDispatch(a, &feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": kind}})
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

func TestPendingFormCancelPreservesToolUserInputBody(t *testing.T) {
	a, _, fc := newTestApp(t)
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:           "input-form-1",
		Kind:         "tool_request_user_input_form",
		SessionKey:   "sess-1",
		OwnerUserID:  "user-1",
		RequestIDRaw: `"req-1"`,
		Status:       "pending",
		PayloadJSON: mustJSON(pendingforms.ToolUserInputPayload{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Questions: []pendingforms.ToolUserInputQuestion{
				{ID: "mode", Question: "Pick one", Options: []pendingforms.ToolUserInputOption{{Label: "Fast"}, {Label: "Safe"}}},
			},
		}),
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	resp, err := a.ServerRequestService().CompletePendingFormCancel( &feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "input-form-1"}})
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("completePendingFormCancel() = %#v, %v", resp, err)
	}
	if len(fc.replyErrors) == 0 {
		t.Fatalf("replyErrors = %+v, want cancel reply to codex", fc.replyErrors)
	}
	card, _ := resp.Card.Data.(map[string]any)
	if got := cardHeaderTitle(t, card); got != "输入请求已取消" {
		t.Fatalf("card title = %q", got)
	}
	body := cardMarkdownContent(t, card)
	for _, want := range []string{"已取消本次补充输入。", "原请求：", "Pick one (`mode`)", "可选值: Fast, Safe"} {
		if !strings.Contains(body, want) {
			t.Fatalf("card body missing %q: %q", want, body)
		}
	}
}

func TestPendingFormCancelPreservesReviewSummary(t *testing.T) {
	a, _, _ := newTestApp(t)

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "review-1",
		Kind:        pendingKindReview,
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(reviewPendingPayload{
			Mode:        reviewFormModeCommit,
			CommitSHA:   "1234567890abcdef",
			CommitTitle: "Fix cancel card rendering",
		}),
	}); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}

	resp, err := completePendingFormCancelDispatch(a, &feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"request_id": "review-1"}})
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("completePendingFormCancel() = %#v, %v", resp, err)
	}
	card, _ := resp.Card.Data.(map[string]any)
	if got := cardHeaderTitle(t, card); got != "Review 已取消" {
		t.Fatalf("card title = %q", got)
	}
	body := cardMarkdownContent(t, card)
	for _, want := range []string{"已取消本次 review 请求。", "模式: commit", "当前选择: `" + appreview.ShortCommitSHA("1234567890abcdef") + "`", "Fix cancel card rendering"} {
		if !strings.Contains(body, want) {
			t.Fatalf("card body missing %q: %q", want, body)
		}
	}
}
