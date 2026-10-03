package backend

import (
	"context"
	"encoding/json"
	"feidex/internal/adapter/backend/codex"
	"feidex/internal/application/backendevents"
	"feidex/internal/domain/turn"
	"strings"
	"testing"
)

type codexErrorEventSink struct {
	recordError   func(string, string, string)
	turnCompleted func(string, string, string)
}

func (s codexErrorEventSink) BindPendingSubmissionTurn(string, string, bool) bool { return false }
func (s codexErrorEventSink) OnTurnStartedNotification(string, string)            {}
func (s codexErrorEventSink) FinishTurn(threadID, turnID, status string) {
	s.TurnCompleted(threadID, turnID, status)
}
func (s codexErrorEventSink) CompleteTurnItem(context.Context, string, string, string, turn.ProtocolItem) {
}
func (s codexErrorEventSink) UpdateInFlightTurnItem(context.Context, string, string, string, turn.ProtocolItem) {
}
func (s codexErrorEventSink) UpdatePendingPlan(string, string)           {}
func (s codexErrorEventSink) ShowStartedProgress(turn.ProtocolItem) bool { return false }
func (s codexErrorEventSink) ProgressEnabled() bool                      { return false }
func (s codexErrorEventSink) RecordTurnError(threadID, turnID, message string) {
	s.RecordError(threadID, turnID, message)
}
func (s codexErrorEventSink) TurnCompleted(threadID, turnID, status string) {
	if s.turnCompleted != nil {
		s.turnCompleted(threadID, turnID, status)
	}
}
func (s codexErrorEventSink) RecordError(threadID, turnID, message string) {
	if s.recordError != nil {
		s.recordError(threadID, turnID, message)
	}
}
func TestTurnCompletedRecordsDiagnosticBeforeCompletion(t *testing.T) {
	var diagnostic string
	completed := false
	owner := codexErrorEventSink{
		recordError: func(threadID, turnID, message string) {
			if threadID != "thread-1" || turnID != "turn-1" {
				t.Fatal("incorrect error binding")
			}
			diagnostic = message
		},
		turnCompleted: func(_, _, status string) {
			completed = true
			if status != "failed" || !strings.Contains(diagnostic, "403") || !strings.Contains(diagnostic, "Forbidden") {
				t.Fatalf("completion lost diagnostic: %q", diagnostic)
			}
		},
	}
	service := backendevents.Service{Deps: backendevents.Dependencies{Lifecycle: owner, Presentation: owner}}
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
