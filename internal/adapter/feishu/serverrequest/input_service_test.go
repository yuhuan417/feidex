package serverrequest

import (
	"testing"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestHandlePendingTextResponseDispatchesKinds(t *testing.T) {
	var svc Service
	if err := svc.HandlePendingTextResponse(nil, nil); err != nil {
		t.Fatalf("HandlePendingTextResponse(nil) error = %v", err)
	}
	if err := svc.HandlePendingTextResponse(&feishu.InboundMessage{}, &state.PendingRequest{Kind: "unknown"}); err != nil {
		t.Fatalf("HandlePendingTextResponse(unknown) error = %v", err)
	}
}
