package clauderuntime

import (
	"context"
	"errors"
	"feidex/internal/domain/conversation"
	"reflect"
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
