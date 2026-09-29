package backend

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTurnCompletedRecordsDiagnosticBeforeCompletion(t *testing.T) {
	var diagnostic string
	completed := false
	router := &CodexEventRouter{
		RecordTurnError: func(threadID, turnID, message string) {
			if threadID != "thread-1" || turnID != "turn-1" {
				t.Fatal("incorrect error binding")
			}
			diagnostic = message
		},
		OnTurnCompleted: func(_, _, status string) {
			completed = true
			if status != "failed" || !strings.Contains(diagnostic, "403") || !strings.Contains(diagnostic, "Forbidden") {
				t.Fatalf("completion lost diagnostic: %q", diagnostic)
			}
		},
	}
	router.HandleNotification("turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"failed","error":{"message":"request failed","codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":403}},"additionalDetails":"Forbidden"}}}`))
	if !completed {
		t.Fatal("missing completion")
	}
}
