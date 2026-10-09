package clauderuntime

import (
	"context"
	"errors"
	"feidex/internal/claudecli"
	"feidex/internal/domain/conversation"
	"reflect"
	"strings"
	"testing"

	domainmodelconfig "feidex/internal/domain/modelconfig"
)

type recordingModelClient struct {
	calls []string
	fail  string
}

func (c *recordingModelClient) SetModel(_ context.Context, value string) error {
	c.calls = append(c.calls, "model:"+value)
	if c.fail == "model" {
		return errors.New("rejected model")
	}
	return nil
}
func (c *recordingModelClient) SetEffort(_ context.Context, value string) error {
	c.calls = append(c.calls, "effort:"+value)
	if c.fail == "effort" {
		return errors.New("rejected effort")
	}
	return nil
}

func TestModelConfigAcknowledgmentStopsOnFailure(t *testing.T) {
	old := domainmodelconfig.Snapshot{Model: "old", Effort: "low"}
	next := domainmodelconfig.Snapshot{Model: "new", Effort: "high"}
	for _, fail := range []string{"", "model", "effort"} {
		t.Run(fail, func(t *testing.T) {
			client := &recordingModelClient{fail: fail}
			err := applyModelSettings(context.Background(), client, old, next)
			if (err != nil) != (fail != "") {
				t.Fatalf("error = %v", err)
			}
			want := []string{"model:new", "effort:high"}
			if fail == "model" {
				want = want[:1]
			}
			if !reflect.DeepEqual(client.calls, want) {
				t.Fatalf("calls = %v", client.calls)
			}
		})
	}
}

func TestModelConfigSafeBoundaryIncludesApprovalsAndBackgroundWork(t *testing.T) {
	for _, kind := range []string{"turn", "interrupt", "approval", "background", "workflow", "background_count", "app_operation", "idle"} {
		t.Run(kind, func(t *testing.T) {
			current := &SessionState{SessionKey: "target", Turns: map[int]*TurnState{}, BackgroundTasks: map[string]*BackgroundTaskState{}}
			s := NewService(Deps{})
			switch kind {
			case "turn":
				current.Turns[1] = &TurnState{TurnID: "turn"}
			case "interrupt":
				current.InterruptPending = true
			case "approval":
				s.pending["request"] = &PendingInteraction{Session: current}
			case "background":
				current.BackgroundTasks["task"] = &BackgroundTaskState{Live: true}
			case "workflow":
				current.PendingWorkflowCount = 1
			case "background_count":
				current.PendingBackgroundAgentCount = 1
			case "app_operation":
				s.deps.Lookup.GetSession = func(string) *conversation.Session { return &conversation.Session{} }
				s.deps.Lookup.SessionHasActiveOps = func(*conversation.Session) bool { return true }
			case "idle":
				s.pending["other"] = &PendingInteraction{Session: &SessionState{SessionKey: "other"}}
				current.BackgroundTasks["done"] = &BackgroundTaskState{Live: false, Notified: true}
			}
			if got := s.modelChangeBlockedReason(current); (got == "") != (kind == "idle") {
				t.Fatalf("reason = %q", got)
			}
		})
	}
}

// TestModelConfigBoundaryClearsWhenBackgroundWorkDrains pins the self-healing
// half of the boundary: the pending counts are a snapshot from the last turn
// end, so they have to fall back to zero once the CLI reports that nothing is
// live. Without it the boundary blocks every submission, and a deferred
// submission means no later turn can ever refresh the counts.
func TestModelConfigBoundaryClearsWhenBackgroundWorkDrains(t *testing.T) {
	s := NewService(Deps{})
	current := &SessionState{SessionKey: "target", Turns: map[int]*TurnState{}, BackgroundTasks: map[string]*BackgroundTaskState{}}
	current.PendingBackgroundAgentCount = 2
	current.PendingWorkflowCount = 1
	if reason := s.modelChangeBlockedReason(current); reason == "" {
		t.Fatal("pending work should block the model change")
	}

	s.HandleBackgroundTasksChanged(current, claudecli.BackgroundTasksChangedEvent{TaskIDs: []string{"task-1"}})
	if reason := s.modelChangeBlockedReason(current); reason == "" {
		t.Fatal("a live background task should still block the model change")
	}

	s.HandleBackgroundTasksChanged(current, claudecli.BackgroundTasksChangedEvent{})
	if reason := s.modelChangeBlockedReason(current); reason != "" {
		t.Fatalf("empty live set should release the boundary, got %q", reason)
	}
	if current.PendingBackgroundAgentCount != 0 || current.PendingWorkflowCount != 0 {
		t.Fatalf("stale counts = %d/%d, want 0/0", current.PendingBackgroundAgentCount, current.PendingWorkflowCount)
	}
}

// TestInterruptReleasesTheModelConfigBoundary covers the documented escape
// hatch: the deferral notice tells the user to retry or /stop, so /stop has to
// leave the session able to start again.
func TestInterruptReleasesTheModelConfigBoundary(t *testing.T) {
	s := NewService(Deps{})
	current := &SessionState{
		SessionKey: "target", Turns: map[int]*TurnState{}, BackgroundTasks: map[string]*BackgroundTaskState{},
		Session: claudecli.NewSession(),
	}
	current.PendingWorkflowCount = 1
	current.BackgroundTasks["task-1"] = &BackgroundTaskState{Live: true}
	s.sessions["target"] = current

	if reason := s.modelChangeBlockedReason(current); reason == "" {
		t.Fatal("pending work should block the model change")
	}
	// The synthetic session is never started, so Interrupt reports ErrNotStarted
	// after it has already released the boundary.
	_ = s.Interrupt(context.Background(), "target")
	// InterruptPending stays set until the interrupted turn actually completes,
	// so the guard may still report the turn. What must be gone is the
	// background-work boundary that nothing else could clear.
	if current.PendingBackgroundAgentCount != 0 || current.PendingWorkflowCount != 0 {
		t.Fatalf("counts = %d/%d after interrupt, want 0/0", current.PendingBackgroundAgentCount, current.PendingWorkflowCount)
	}
	for taskID, task := range current.BackgroundTasks {
		if task != nil && task.Live {
			t.Fatalf("task %s still live after interrupt", taskID)
		}
	}
	if reason := s.modelChangeBlockedReason(current); strings.Contains(reason, "后台任务") {
		t.Fatalf("background boundary survived the interrupt: %q", reason)
	}
}
