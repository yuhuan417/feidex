package backend

import (
	"context"
	"encoding/json"
	"feidex/internal/adapter/backend/codex"
	"feidex/internal/application/backendevents"
	"strings"
	"testing"
)

func TestTurnCompletedRecordsDiagnosticBeforeCompletion(t *testing.T) {
	var diagnostic string
	completed := false
	service := backendevents.Service{
		RecordError: func(threadID, turnID, message string) {
			if threadID != "thread-1" || turnID != "turn-1" {
				t.Fatal("incorrect error binding")
			}
			diagnostic = message
		},
		TurnCompleted: func(_, _, status string) {
			completed = true
			if status != "failed" || !strings.Contains(diagnostic, "403") || !strings.Contains(diagnostic, "Forbidden") {
				t.Fatalf("completion lost diagnostic: %q", diagnostic)
			}
		},
	}
	event, handled, err := codex.DecodeNotification("turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"failed","error":{"message":"request failed","codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":403}},"additionalDetails":"Forbidden"}}}`))
	if err != nil || !handled {
		t.Fatalf("decode: %v", err)
	}
	if _, err := service.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if !completed {
		t.Fatal("missing completion")
	}
}
