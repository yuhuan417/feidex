package app

import (
	"context"
	"testing"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestSteerHelperBranches(t *testing.T) {
	a, _, fc := newTestApp(t)
	if got := a.replyRootTurnLink(nil); got != nil {
		t.Fatalf("replyRootTurnLink(nil) = %+v, want nil", got)
	}
	if got := a.sessionKeyForInboundMessage(&feishu.InboundMessage{ChatID: "chat", ChatType: "p2p", UserID: "u"}, &state.MessageLink{SessionKey: "sess-1"}); got != "sess-1" {
		t.Fatalf("sessionKeyForInboundMessage() = %q, want sess-1", got)
	}
	if got := a.pendingInputSessionKey(nil); got != "" {
		t.Fatalf("pendingInputSessionKey(nil) = %q, want empty", got)
	}
	if got, err := a.trySteerInboundReply(nil, nil); got || err != nil {
		t.Fatalf("trySteerInboundReply(nil) = %v, %v", got, err)
	}

	link := &state.MessageLink{ThreadID: "thread-1", TurnID: "turn-1", SessionKey: "sess-1"}
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat", ChatType: "p2p", UserID: "u", Text: "hello"}
	fc.callHook = func(_ context.Context, method string, _ any, _ any) error {
		if method == "turn/steer" {
			return nil
		}
		return nil
	}
	got, err := a.trySteerInboundReply(msg, link)
	if err != nil || !got {
		t.Fatalf("trySteerInboundReply() = %v, %v", got, err)
	}
}
