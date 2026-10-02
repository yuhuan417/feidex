package codex

import (
	"encoding/json"
	"testing"

	"feidex/internal/application"
	"feidex/internal/codexrpc"
	"feidex/internal/domain/interaction"
)

func TestRequestDecodePreservesResponseToken(t *testing.T) {
	for _, id := range []string{`123`, `"123"`, `"request-9"`} {
		event := DecodeRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(id), Method: "item/tool/requestUserInput", Params: json.RawMessage(`{"threadId":"t","turnId":"u","questions":[{"id":"q","question":"Which?"}]}`)})
		if event.ResponseToken != id || event.Kind != application.EventUserInputRequested {
			t.Fatalf("request %s: %#v", id, event)
		}
		if payload, ok := event.Payload.(interaction.ToolUserInputPayload); !ok || len(payload.Questions) != 1 || payload.Questions[0].ID != "q" {
			t.Fatalf("payload=%#v", event.Payload)
		}
		resolved, handled, err := DecodeNotification("serverRequest/resolved", json.RawMessage(`{"requestId":`+id+`}`))
		if !handled || err != nil || resolved.RequestID != event.RequestID {
			t.Fatalf("resolve %s: event=%#v handled=%v error=%v", id, resolved, handled, err)
		}
	}
}

func TestMalformedRequestBecomesProtocolRejection(t *testing.T) {
	event := DecodeRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(`77`), Method: "item/permissions/requestApproval", Params: json.RawMessage(`{`)})
	rejection, ok := event.Payload.(application.RequestRejected)
	if event.Kind != application.EventRequestRejected || event.ResponseToken != "77" || !ok || rejection.Code != -32602 {
		t.Fatalf("event=%#v", event)
	}
}
