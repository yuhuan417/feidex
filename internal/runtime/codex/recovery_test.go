package codexruntime

import (
	"reflect"
	"testing"
)

func TestResumeQueuedSessionsUsesSessionActor(t *testing.T) {
	var admitted []string
	var started []string
	svc := RecoveryService{
		SessionKeysForRecovery: func() []string {
			return []string{"session-a", "", "session-b", "session-a"}
		},
		SessionShouldStartNextSubmissionAsync: func(sessionKey string) bool {
			return sessionKey != "session-b"
		},
		StartNextSubmissionAsync: func(sessionKey, reason string) {
			if reason != "codexRuntimeRecovered" {
				t.Errorf("recovery reason = %q", reason)
			}
			started = append(started, sessionKey)
		},
		RunSessionAsync: func(sessionKey string, fn func()) {
			admitted = append(admitted, sessionKey)
			fn()
		},
	}

	svc.ResumeQueuedSessions()
	if want := []string{"session-a", "session-a"}; !reflect.DeepEqual(admitted, want) {
		t.Fatalf("session actor admissions = %v, want %v", admitted, want)
	}
	if want := []string{"session-a", "session-a"}; !reflect.DeepEqual(started, want) {
		t.Fatalf("recovery starts = %v, want %v", started, want)
	}
}

func TestResumeQueuedSessionsRequiresActor(t *testing.T) {
	started := make(chan string, 1)
	svc := RecoveryService{
		SessionKeysForRecovery:                func() []string { return []string{"session-a"} },
		SessionShouldStartNextSubmissionAsync: func(string) bool { return true },
		StartNextSubmissionAsync:              func(sessionKey, _ string) { started <- sessionKey },
	}
	svc.ResumeQueuedSessions()
	select {
	case got := <-started:
		t.Fatalf("unowned recovery started session = %q", got)
	default:
	}
}
