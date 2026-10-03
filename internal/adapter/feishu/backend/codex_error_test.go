package backend

import (
	"context"
	"encoding/json"
	"feidex/internal/adapter/backend/codex"
	"feidex/internal/application"
	"feidex/internal/application/backendevents"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/turn"
	"strings"
	"testing"
)

type codexErrorEventSink struct {
	recordError   func(string, string, string)
	turnCompleted func(string, string, string)
}

func (s codexErrorEventSink) ItemStarted(context.Context, string, string, turn.ProtocolItem)   {}
func (s codexErrorEventSink) ItemCompleted(context.Context, string, string, turn.ProtocolItem) {}
func (s codexErrorEventSink) ItemProgress(context.Context, string, string, turn.ProtocolItem)  {}
func (s codexErrorEventSink) PlanUpdated(string, string)                                       {}
func (s codexErrorEventSink) TurnStarted(string, string)                                       {}
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
func (s codexErrorEventSink) FailCompact(string, string, string) bool            { return false }
func (s codexErrorEventSink) FailSubmission(string, string)                      {}
func (s codexErrorEventSink) UsageUpdated(string, string, turn.ThreadTokenUsage) {}
func (s codexErrorEventSink) GoalUpdated(string, conversation.ThreadGoal)        {}
func (s codexErrorEventSink) GoalCleared(string)                                 {}
func (s codexErrorEventSink) RequestResolved(string)                             {}
func (s codexErrorEventSink) InteractionRequested(context.Context, application.BackendEvent) error {
	return nil
}

func TestTurnCompletedRecordsDiagnosticBeforeCompletion(t *testing.T) {
	var diagnostic string
	completed := false
	service := backendevents.Service{Sink: codexErrorEventSink{
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
	}}
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
