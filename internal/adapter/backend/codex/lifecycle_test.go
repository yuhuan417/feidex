package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"feidex/internal/application"
)

func TestDecodeLifecyclePreservesBindingAndAuthoritativeBoundaries(t *testing.T) {
	for _, tc := range []struct {
		method, wire, kind, turn, request, status string
	}{
		{"turn/started", `{"threadId":"thread","turnId":"fallback","turn":{"id":"nested"}}`, application.EventTurnStarted, "nested", "", ""},
		{"turn/started", `{"threadId":"thread","turnId":"fallback"}`, application.EventTurnStarted, "fallback", "", ""},
		{"turn/completed", `{"threadId":"thread","turn":{"id":"completed","status":"interrupted"}}`, application.EventTurnCompleted, "completed", "", "interrupted"},
		{"serverRequest/resolved", `{"threadId":"thread","requestId":42}`, application.EventRequestResolved, "", "42", ""},
		{"serverRequest/resolved", `{"threadId":"thread","requestId":"string-id"}`, application.EventRequestResolved, "", "string-id", ""},
		{"error", `{"threadId":"thread","turnId":"failed","error":{"message":"failure"}}`, application.EventTurnError, "failed", "", ""},
	} {
		event, handled, err := DecodeLifecycle(tc.method, json.RawMessage(tc.wire))
		if err != nil || !handled || event.Kind != tc.kind || event.ThreadID != "thread" || event.TurnID != tc.turn || event.RequestID != tc.request || event.Status != tc.status || event.Payload != nil {
			t.Errorf("DecodeLifecycle(%s) = %+v, handled=%v, err=%v", tc.method, event, handled, err)
		}
	}
}

func TestDecodeCompletionRetainsErrorDiagnostic(t *testing.T) {
	event, _, err := DecodeLifecycle("turn/completed", json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"failed","error":{"message":"request failed","codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":403}},"additionalDetails":"Forbidden"}}}`))
	if err != nil || !strings.Contains(event.Message, "403") || !strings.Contains(event.Message, "Forbidden") {
		t.Fatalf("diagnostic lost: %+v, %v", event, err)
	}
}

func TestDecodeLifecycleRejectsMalformedRecognizedEvent(t *testing.T) {
	if _, handled, err := DecodeLifecycle("turn/completed", json.RawMessage(`{`)); !handled || err == nil {
		t.Fatal("malformed recognized event was accepted")
	}
	if _, handled, err := DecodeLifecycle("item/completed", nil); handled || err != nil {
		t.Fatal("unsupported family must stay with the legacy adapter")
	}
}
