package app

import (
	"testing"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestHandlePendingTextResponseDispatchesKinds(t *testing.T) {
	a, _, _ := newTestApp(t)
	if err := a.ServerRequestService().HandlePendingTextResponse(nil, nil); err != nil {
		t.Fatalf("handlePendingTextResponse(nil) error = %v", err)
	}
	if err := a.ServerRequestService().HandlePendingTextResponse(&feishu.InboundMessage{}, &state.PendingRequest{Kind: "unknown"}); err != nil {
		t.Fatalf("handlePendingTextResponse(unknown) error = %v", err)
	}
}
