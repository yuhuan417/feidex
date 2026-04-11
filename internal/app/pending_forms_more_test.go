package app

import (
	"testing"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestHandlePendingTextResponseDispatchesKinds(t *testing.T) {
	a, _, _ := newTestApp(t)
	if err := a.handlePendingTextResponse(nil, nil); err != nil {
		t.Fatalf("handlePendingTextResponse(nil) error = %v", err)
	}
	if err := a.handlePendingTextResponse(&feishu.InboundMessage{}, &state.PendingRequest{Kind: "unknown"}); err != nil {
		t.Fatalf("handlePendingTextResponse(unknown) error = %v", err)
	}
}
