package backend

import (
	"context"
	"testing"

	appturnstream "feidex/internal/app/turnstream"
	"feidex/internal/state"
)

func TestFailSubmissionWithoutTerminalCompletionDoesNotFallbackWhenQueuedSelectorBlocks(t *testing.T) {
	sess := &state.Session{
		Key:                "sess-1",
		ActiveThreadID:     "thread-1",
		ActiveSubmissionID: "sub-1",
		ActiveTurnID:       "turn-1",
		Status:             state.SessionStatusTurnInProgress.String(),
		Queue:              []string{"queued-1"},
		ActiveOperations: []state.SessionActiveOperation{{
			SubmissionID: "sub-1",
			ThreadID:     "thread-1",
			TurnID:       "turn-1",
		}},
	}
	sub := &state.Submission{
		ID:         "sub-1",
		SessionKey: "sess-1",
		ThreadID:   "thread-1",
		TurnID:     "turn-1",
		Status:     state.SubmissionStatusRunning.String(),
	}
	var selectorCalls int
	var startCalls int
	svc := NewBackendFailureService(FailureDeps{
		State: FailureStateDeps{
			GetSubmission: func(id string) *state.Submission {
				if id != sub.ID {
					return nil
				}
				cp := *sub
				return &cp
			},
			FinalizeSubmission: func(id, status string) error {
				if id == sub.ID {
					sub.Status = status
					sub.Finalized = true
				}
				return nil
			},
			UpdateSession: func(key string, mutate func(*state.Session)) (*state.Session, error) {
				if key != sess.Key {
					return nil, nil
				}
				if mutate != nil {
					mutate(sess)
				}
				cp := *sess
				cp.Queue = append([]string(nil), sess.Queue...)
				cp.ActiveOperations = append([]state.SessionActiveOperation(nil), sess.ActiveOperations...)
				return &cp, nil
			},
		},
		Runtime: FailureRuntimeDeps{
			FlushTurnStream: func(context.Context, string, string) appturnstream.FlushResult {
				return appturnstream.FlushResult{}
			},
		},
		Cards: FailureCardDeps{
			ObserveAutoRetryTerminal: func(string, string, string, *state.Session, *state.Submission, string) bool {
				return true
			},
		},
		Async: FailureAsyncDeps{
			NextQueuedSubmissionSessionKey: func(string) string {
				selectorCalls++
				return ""
			},
			StartNextSubmissionAsync: func(string, string) {
				startCalls++
			},
			RunAsync: func(fn func()) {
				fn()
			},
		},
	})

	svc.FailSubmissionWithoutTerminalCompletion("sess-1", sub, "thread-1", "turn-1", "backend failed")

	if selectorCalls != 1 {
		t.Fatalf("NextQueuedSubmissionSessionKey calls = %d, want 1", selectorCalls)
	}
	if startCalls != 0 {
		t.Fatalf("StartNextSubmissionAsync calls = %d, want 0 when selector blocks", startCalls)
	}
}
