package app

import (
	"context"
	"testing"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestPendingFormTurnAppendHelpers(t *testing.T) {
	a, ff, fc := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	_ = sub

	pending := &state.PendingRequest{
		ID:          "append-1",
		Kind:        "turn_append",
		SessionKey:  "sess-1",
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		OwnerUserID: "user-1",
		FeishuMsgID: "card-1",
		Status:      "pending",
	}
	if err := a.store.UpsertPending(pending); err != nil {
		t.Fatalf("UpsertPending() error = %v", err)
	}
	called := false
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method == "turn/steer" {
			called = true
		}
		_ = params
		_ = out
		return nil
	}

	msg := &feishu.InboundMessage{MessageID: "msg-1", ChatType: "group", Text: "append text"}
	if err := a.handlePendingTextResponse(msg, pending); err != nil {
		t.Fatalf("handlePendingTextResponse() error = %v", err)
	}
	if !called {
		t.Fatal("expected turn/steer call")
	}
	if req := a.store.PendingByID("append-1"); req == nil || req.Status != "resolved" {
		t.Fatalf("pending status = %+v, want resolved", req)
	}
	if len(ff.patchedCards) == 0 || len(ff.replyTexts) == 0 {
		t.Fatalf("turn append did not patch/respond: patched=%d reply=%d", len(ff.patchedCards), len(ff.replyTexts))
	}

	if err := a.store.UpsertPending(&state.PendingRequest{ID: "append-2", Kind: "turn_append", SessionKey: "sess-1", OwnerUserID: "user-1", FeishuMsgID: "card-2", Status: "pending", CreatedAt: time.Now().Unix()}); err != nil {
		t.Fatalf("UpsertPending(append-2) error = %v", err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{ID: "append-3", Kind: "turn_append", SessionKey: "sess-1", OwnerUserID: "user-1", FeishuMsgID: "card-3", Status: "pending", CreatedAt: time.Now().Unix()}); err != nil {
		t.Fatalf("UpsertPending(append-3) error = %v", err)
	}
	a.resolvePendingTurnAppendRequests("sess-1", "user-1")
	if req := a.store.PendingByID("append-2"); req == nil || req.Status != "resolved" {
		t.Fatalf("append-2 status = %+v, want resolved", req)
	}
	if req := a.store.PendingByID("append-3"); req == nil || req.Status != "resolved" {
		t.Fatalf("append-3 status = %+v, want resolved", req)
	}
}

func TestHandlePendingTextResponseDispatchesKinds(t *testing.T) {
	a, _, _ := newTestApp(t)
	if err := a.handlePendingTextResponse(nil, nil); err != nil {
		t.Fatalf("handlePendingTextResponse(nil) error = %v", err)
	}
	if err := a.handlePendingTextResponse(&feishu.InboundMessage{}, &state.PendingRequest{Kind: "unknown"}); err != nil {
		t.Fatalf("handlePendingTextResponse(unknown) error = %v", err)
	}
}
