package codex

import (
	"encoding/json"
	"testing"

	"feidex/internal/application"
	"feidex/internal/codexrpc"
)

func TestRequestDecodePreservesResponseToken(t *testing.T) {
	for _, id := range []string{`123`, `"123"`, `"request-9"`} {
		event := DecodeRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(id), Method: "item/tool/requestUserInput", Params: json.RawMessage(`{"threadId":"t","turnId":"u","questions":[{"id":"q","question":"Which?"}]}`)})
		if event.ResponseToken != id || event.Kind != application.EventUserInputRequested {
			t.Fatalf("request %s: %#v", id, event)
		}
		if event.UserInput == nil || len(event.UserInput.Questions) != 1 || event.UserInput.Questions[0].ID != "q" {
			t.Fatalf("user input=%#v", event.UserInput)
		}
		resolved, handled, err := DecodeNotification("serverRequest/resolved", json.RawMessage(`{"requestId":`+id+`}`))
		if !handled || err != nil || resolved.RequestID != event.RequestID {
			t.Fatalf("resolve %s: event=%#v handled=%v error=%v", id, resolved, handled, err)
		}
	}
}

func TestMalformedRequestBecomesProtocolRejection(t *testing.T) {
	event := DecodeRequest(codexrpc.RequestEnvelope{ID: json.RawMessage(`77`), Method: "item/permissions/requestApproval", Params: json.RawMessage(`{`)})
	if event.Kind != application.EventRequestRejected || event.ResponseToken != "77" || event.Rejected == nil || event.Rejected.Code != -32602 {
		t.Fatalf("event=%#v", event)
	}
}
