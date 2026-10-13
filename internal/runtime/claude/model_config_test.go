package clauderuntime

import (
	"context"
	"errors"
	"feidex/internal/claudecli"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestModelConfigSafeBoundaryOnlyBlocksForegroundWork(t *testing.T) {
	for _, tc := range []struct {
		kind    string
		blocked bool
	}{
		{"turn", true}, {"current_turn", true}, {"interrupt", true}, {"foreground_approval", true},
		{"app_operation", true}, {"compacting", true}, {"turn_starting", true},
		{"detached_approval", false}, {"background", false}, {"notified_background", false},
		{"workflow", false}, {"background_count", false}, {"stale_counts", false}, {"idle", false},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			current := &SessionState{SessionKey: "target", Turns: map[int]*TurnState{}, BackgroundTasks: map[string]*BackgroundTaskState{}}
			s := NewService(Deps{})
			switch tc.kind {
			case "turn":
				current.Turns[1] = &TurnState{TurnID: "turn"}
			case "current_turn":
				current.CurrentTurnNumber = 1
			case "interrupt":
				current.InterruptPending = true
			case "foreground_approval":
				current.Turns[1] = &TurnState{TurnID: "turn"}
				s.pending["request"] = &PendingInteraction{Session: current}
			case "detached_approval":
				s.pending["request"] = &PendingInteraction{Session: current}
			case "background":
				current.BackgroundTasks["task"] = &BackgroundTaskState{Live: true}
			case "notified_background":
				current.BackgroundTasks["task"] = &BackgroundTaskState{Live: true, Notified: true}
			case "workflow":
				current.PendingWorkflowCount = 1
			case "background_count":
				current.PendingBackgroundAgentCount = 1
			case "stale_counts":
				current.PendingBackgroundAgentCount, current.PendingWorkflowCount = 3, 2
			case "app_operation":
				s.deps.Lookup.GetSession = func(string) *conversation.Session { return &conversation.Session{} }
				s.deps.Lookup.SessionHasActiveOps = func(*conversation.Session) bool { return true }
			case "compacting", "turn_starting":
				status := conversation.SessionStatusCompacting
				if tc.kind == "turn_starting" {
					status = conversation.SessionStatusTurnStarting
				}
				s.deps.Lookup.GetSession = func(string) *conversation.Session { return &conversation.Session{Status: string(status)} }
				s.deps.Lookup.SessionHasActiveOps = func(*conversation.Session) bool { return false }
			case "idle":
				s.pending["other"] = &PendingInteraction{Session: &SessionState{SessionKey: "other"}}
				current.BackgroundTasks["done"] = &BackgroundTaskState{Live: false, Notified: true}
			}
			if got := s.modelChangeBlockedReason(current); (got != "") != tc.blocked {
				t.Fatalf("reason = %q", got)
			}
		})
	}
}

func TestEnsureSessionAppliesModelConfigAfterForegroundCompletesWithBackgroundWork(t *testing.T) {
	for _, kind := range []string{"model", "effort", "custom_model", "auxiliary_model", "default_effort"} {
		t.Run(kind, func(t *testing.T) {
			// This local protocol fixture acknowledges controls without starting
			// a real Claude turn or consuming tokens.
			command := filepath.Join(t.TempDir(), "claude")
			const script = `#!/bin/sh
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$0.requests"
  request_id=$(printf '%s' "$line" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
  printf '{"type":"control_response","response":{"subtype":"success","request_id":"%s","response":{}}}\n' "$request_id"
done
`
			if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			s := NewService(Deps{})
			s.UpdateConfig(config.ClaudeConfig{Command: command, Model: "sonnet", Effort: "high"})
			t.Cleanup(func() { _ = s.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ws := &config.Workspace{ID: "workspace", Cwd: t.TempDir()}
			const conversationID = "existing-conversation"
			if _, err := s.EnsureSession(ctx, "target", ws, conversationID, ""); err != nil {
				t.Fatal(err)
			}
			current, err := s.SessionState("target")
			if err != nil {
				t.Fatal(err)
			}
			current.Mu.Lock()
			current.Turns[1] = &TurnState{}
			current.CurrentTurnNumber = 1
			current.PendingBackgroundAgentCount, current.PendingWorkflowCount = 2, 1
			current.BackgroundTasks["agent"] = &BackgroundTaskState{Live: true}
			current.Mu.Unlock()
			pending := &PendingInteraction{Session: current}
			s.storePending("detached", pending)
			desired := config.ClaudeConfig{Command: command, Model: "sonnet", Effort: "high"}
			restart := false
			switch kind {
			case "model":
				desired.Model = "opus"
			case "effort":
				desired.Effort = "low"
			case "custom_model":
				desired.Model, restart = "custom-model", true
			case "auxiliary_model":
				desired.SubagentModel, restart = "haiku", true
			case "default_effort":
				desired.Effort, restart = "", true
			}
			s.UpdateConfig(desired)
			if _, err := s.EnsureSession(ctx, "target", ws, conversationID, ""); !errors.Is(err, claudecli.ErrModelConfigApply) {
				t.Fatalf("active foreground model change error = %v", err)
			}
			s.HandleTurnComplete(current, claudecli.TurnCompleteEvent{TurnNumber: 1, Success: true})
			id, err := s.EnsureSession(ctx, "target", ws, conversationID, "")
			if err != nil || id != conversationID {
				t.Fatalf("foreground completed: id = %q, error = %v", id, err)
			}
			next, err := s.SessionState("target")
			if err != nil {
				t.Fatal(err)
			}
			if (next != current) != restart {
				t.Fatalf("process replaced = %v, want %v", next != current, restart)
			}
			next.Mu.Lock()
			applied := next.AppliedModelConfig
			next.Mu.Unlock()
			if applied != modelSettingsFromConfig(desired, desired.Model) {
				t.Fatalf("applied config = %#v", applied)
			}
			s.mu.Lock()
			remaining := s.pending["detached"]
			s.mu.Unlock()
			if (!restart && remaining != pending) || (restart && remaining != nil) {
				t.Fatalf("pending interaction after change = %p, restart = %v", remaining, restart)
			}
			requests, err := os.ReadFile(command + ".requests")
			if err != nil {
				t.Fatal(err)
			}
			wantInitializations := 1
			if restart {
				wantInitializations = 2
			}
			if got := strings.Count(string(requests), `"subtype":"initialize"`); got != wantInitializations {
				t.Fatalf("initializations = %d, want %d", got, wantInitializations)
			}
		})
	}
}
