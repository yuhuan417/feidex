package clauderuntime

import (
	"context"
	"testing"

	"feidex/internal/claudecli"
	"feidex/internal/state"
)

func TestWithClaudeModelEnv(t *testing.T) {
	env := withClaudeModelEnv([]string{
		"PATH=/usr/bin",
		"ANTHROPIC_MODEL=old-model",
		"ANTHROPIC_MODEL=duplicate-old-model",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=old-small-model",
	}, " deepseek-flash[1m] ", " deepseek-flash ", " subagent-model ")

	got := make(map[string][]string)
	for _, entry := range env {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				got[entry[:i]] = append(got[entry[:i]], entry[i+1:])
				break
			}
		}
	}

	want := map[string]string{
		"PATH":                           "/usr/bin",
		"ANTHROPIC_MODEL":                "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "deepseek-flash",
		"CLAUDE_CODE_SUBAGENT_MODEL":     "subagent-model",
	}
	for key, value := range want {
		values := got[key]
		if len(values) != 1 || values[0] != value {
			t.Fatalf("%s = %#v, want [%q]", key, values, value)
		}
	}
}

func TestWithClaudeModelEnvDefaultsAuxiliaryModelsToPrimaryModel(t *testing.T) {
	env := withClaudeModelEnv(nil, "deepseek-flash[1m]", "", "")
	want := map[string]string{
		"ANTHROPIC_MODEL":                "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "deepseek-flash[1m]",
		"CLAUDE_CODE_SUBAGENT_MODEL":     "deepseek-flash[1m]",
	}
	for _, entry := range env {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				key, value := entry[:i], entry[i+1:]
				if want[key] != value {
					t.Fatalf("%s = %q, want %q", key, value, want[key])
				}
				delete(want, key)
				break
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing defaulted model environment variables: %#v", want)
	}
}

func TestWithClaudeModelEnvLeavesClaudeBuiltinAuxiliaryDefaultsUnset(t *testing.T) {
	for _, model := range []string{"opus", "sonnet", "haiku", "claude-sonnet-4-5-20250929"} {
		env := withClaudeModelEnv(nil, model, "", "")
		if len(env) != 1 || env[0] != "ANTHROPIC_MODEL="+model {
			t.Fatalf("model %q environment = %#v, want only ANTHROPIC_MODEL", model, env)
		}
	}
}

func TestWithClaudeModelEnvKeepsExplicitAuxiliaryModelsForClaudeBuiltin(t *testing.T) {
	env := withClaudeModelEnv(nil, "sonnet", "custom-haiku", "custom-subagent")
	want := map[string]string{
		"ANTHROPIC_MODEL":               "sonnet",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": "custom-haiku",
		"CLAUDE_CODE_SUBAGENT_MODEL":    "custom-subagent",
	}
	for _, entry := range env {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				key, value := entry[:i], entry[i+1:]
				if want[key] != value {
					t.Fatalf("%s = %q, want %q", key, value, want[key])
				}
				delete(want, key)
				break
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing explicit auxiliary environment variables: %#v", want)
	}
}

func TestHandleBackgroundTaskEventCapturesTargetAndNotifiesOnce(t *testing.T) {
	var delivered []BackgroundTaskTarget
	var deliveredEvents []claudecli.BackgroundTaskEvent
	svc := NewService(Deps{
		Delivery: DeliveryDeps{
			SendBackgroundTaskNotification: func(_ context.Context, target BackgroundTaskTarget, event claudecli.BackgroundTaskEvent) {
				delivered = append(delivered, target)
				deliveredEvents = append(deliveredEvents, event)
			},
		},
		Lookup: LookupDeps{
			FindSubmissionByTurn: func(threadID, turnID string) (string, *state.Submission) {
				if threadID != "thread-1" || turnID != "turn-1" {
					return "", nil
				}
				return "session-1", &state.Submission{
					SessionKey:       "session-1",
					WorkspaceID:      "workspace-1",
					ChatID:           "chat-1",
					TriggerMessageID: "message-1",
					UserID:           "user-1",
				}
			},
		},
	})
	runtimeState := &SessionState{
		SessionKey:        "session-1",
		SessionID:         "thread-1",
		CurrentTurnNumber: 3,
		Turns: map[int]*TurnState{
			3: {TurnNumber: 3, TurnID: "turn-1"},
		},
		BackgroundTasks: map[string]*BackgroundTaskState{},
	}

	svc.HandleBackgroundTaskEvent(runtimeState, claudecli.BackgroundTaskEvent{
		Subtype:        "task_started",
		TaskID:         "task-1",
		ToolUseID:      "tool-1",
		Description:    "inspect repository",
		IsBackgrounded: true,
	})
	svc.HandleBackgroundTaskEvent(runtimeState, claudecli.BackgroundTaskEvent{
		Subtype:   "task_notification",
		TaskID:    "task-1",
		Status:    "completed",
		Summary:   "done",
		ToolUseID: "tool-1",
	})
	// Claude can deliver the same completion notification more than once when
	// the parent stream and background-task stream converge.
	svc.HandleBackgroundTaskEvent(runtimeState, claudecli.BackgroundTaskEvent{
		Subtype:   "task_notification",
		TaskID:    "task-1",
		Status:    "completed",
		ToolUseID: "tool-1",
	})

	if len(delivered) != 1 || len(deliveredEvents) != 1 {
		t.Fatalf("delivery count = %d/%d, want 1/1", len(delivered), len(deliveredEvents))
	}
	target := delivered[0]
	if target.SessionKey != "session-1" || target.WorkspaceID != "workspace-1" || target.ChatID != "chat-1" || target.TriggerMessageID != "message-1" {
		t.Fatalf("captured target = %#v", target)
	}
	if target.TaskID != "task-1" || target.ToolUseID != "tool-1" || target.TurnID != "turn-1" || !target.IsBackgrounded {
		t.Fatalf("captured task target = %#v", target)
	}
	if deliveredEvents[0].Description != "inspect repository" || deliveredEvents[0].Summary != "done" {
		t.Fatalf("delivered event = %#v", deliveredEvents[0])
	}
}

func TestHandleBackgroundTaskEventCanNotifyAfterParentSubmissionIsGone(t *testing.T) {
	var delivered BackgroundTaskTarget
	svc := NewService(Deps{
		Delivery: DeliveryDeps{
			SendBackgroundTaskNotification: func(_ context.Context, target BackgroundTaskTarget, _ claudecli.BackgroundTaskEvent) {
				delivered = target
			},
		},
		Lookup: LookupDeps{
			FindSubmissionByTurn: func(string, string) (string, *state.Submission) {
				return "session-1", &state.Submission{
					WorkspaceID:      "workspace-1",
					ChatID:           "chat-1",
					TriggerMessageID: "message-1",
				}
			},
		},
	})
	runtimeState := &SessionState{
		SessionKey:        "session-1",
		SessionID:         "thread-1",
		CurrentTurnNumber: 1,
		Turns:             map[int]*TurnState{1: {TurnID: "turn-1"}},
	}
	svc.HandleBackgroundTaskEvent(runtimeState, claudecli.BackgroundTaskEvent{
		Subtype:        "task_started",
		TaskID:         "task-1",
		IsBackgrounded: true,
	})
	// The parent submission is no longer needed after task_started captured its
	// delivery target.
	svc.deps.Lookup.FindSubmissionByTurn = func(string, string) (string, *state.Submission) { return "", nil }
	svc.HandleBackgroundTaskEvent(runtimeState, claudecli.BackgroundTaskEvent{
		Subtype: "task_notification",
		TaskID:  "task-1",
		Status:  "completed",
	})
	if delivered.ChatID != "chat-1" || delivered.TriggerMessageID != "message-1" {
		t.Fatalf("delivery target after parent cleanup = %#v", delivered)
	}
}

func TestHandleBackgroundTasksChangedPreservesTargetsAndReconcilesLiveSet(t *testing.T) {
	svc := NewService(Deps{})
	runtimeState := &SessionState{
		SessionKey: "session-1",
		SessionID:  "thread-1",
		BackgroundTasks: map[string]*BackgroundTaskState{
			"task-1": {
				Target: BackgroundTaskTarget{
					TaskID:           "task-1",
					ChatID:           "chat-1",
					TriggerMessageID: "message-1",
					Description:      "old description",
				},
				Live: true,
			},
			"task-notified": {
				Target:   BackgroundTaskTarget{TaskID: "task-notified"},
				Notified: true,
				Live:     true,
			},
		},
	}

	svc.HandleBackgroundTasksChanged(runtimeState, claudecli.BackgroundTasksChangedEvent{TaskIDs: []string{"task-1", "task-2"}})

	runtimeState.Mu.Lock()
	if got := runtimeState.BackgroundTasks["task-1"]; got == nil || got.Target.ChatID != "chat-1" || got.Target.TriggerMessageID != "message-1" || got.Target.Description != "old description" || !got.Live {
		t.Fatalf("preserved task target = %#v", got)
	}
	unknown := runtimeState.BackgroundTasks["task-2"]
	if unknown == nil || unknown.Target.ChatID != "" || unknown.Target.TriggerMessageID != "" || unknown.Target.Description != "" || !unknown.Live {
		t.Fatalf("id-only task state = %#v", unknown)
	}
	if _, ok := runtimeState.BackgroundTasks["task-notified"]; ok {
		t.Fatalf("notified stale task remains in snapshot state: %#v", runtimeState.BackgroundTasks)
	}
	runtimeState.Mu.Unlock()

	// A snapshot can remove a task before its edge notification. Keep the
	// unnotified target for that notification, but mark it no longer live.
	svc.HandleBackgroundTasksChanged(runtimeState, claudecli.BackgroundTasksChangedEvent{})
	runtimeState.Mu.Lock()
	deferred := runtimeState.BackgroundTasks["task-1"]
	if deferred == nil || deferred.Live {
		t.Fatalf("unnotified stale task = %#v, want retained and non-live", deferred)
	}
	runtimeState.Mu.Unlock()
}

func TestHandleTurnDurationStoresPendingBackgroundWork(t *testing.T) {
	svc := NewService(Deps{})
	runtimeState := &SessionState{}
	svc.HandleTurnDuration(runtimeState, claudecli.TurnDurationEvent{
		DurationMs:                  1234,
		BudgetTokens:                42,
		BudgetLimit:                 100,
		BudgetNudges:                3,
		MessageCount:                9,
		PendingBackgroundAgentCount: 2,
		PendingWorkflowCount:        1,
	})

	runtimeState.Mu.Lock()
	defer runtimeState.Mu.Unlock()
	if runtimeState.LastTurnDurationMs != 1234 || runtimeState.LastTurnBudgetTokens != 42 || runtimeState.LastTurnBudgetLimit != 100 || runtimeState.LastTurnBudgetNudges != 3 || runtimeState.LastTurnMessageCount != 9 {
		t.Fatalf("turn metadata = %+v", runtimeState)
	}
	if runtimeState.PendingBackgroundAgentCount != 2 || runtimeState.PendingWorkflowCount != 1 {
		t.Fatalf("pending work counts = %d/%d", runtimeState.PendingBackgroundAgentCount, runtimeState.PendingWorkflowCount)
	}
}

func TestBackgroundTaskNotificationStillDeliversAfterTurnDuration(t *testing.T) {
	var delivered int
	svc := NewService(Deps{
		Delivery: DeliveryDeps{
			SendBackgroundTaskNotification: func(context.Context, BackgroundTaskTarget, claudecli.BackgroundTaskEvent) {
				delivered++
			},
		},
		Lookup: LookupDeps{
			FindSubmissionByTurn: func(string, string) (string, *state.Submission) {
				return "session-1", &state.Submission{WorkspaceID: "workspace-1", ChatID: "chat-1", TriggerMessageID: "message-1"}
			},
		},
	})
	runtimeState := &SessionState{
		SessionKey:        "session-1",
		SessionID:         "thread-1",
		CurrentTurnNumber: 1,
		Turns:             map[int]*TurnState{1: {TurnID: "turn-1"}},
		BackgroundTasks:   map[string]*BackgroundTaskState{},
	}
	svc.HandleTurnDuration(runtimeState, claudecli.TurnDurationEvent{PendingBackgroundAgentCount: 1})
	svc.HandleBackgroundTaskEvent(runtimeState, claudecli.BackgroundTaskEvent{Subtype: "task_started", TaskID: "task-1", IsBackgrounded: true})
	svc.HandleBackgroundTaskEvent(runtimeState, claudecli.BackgroundTaskEvent{Subtype: "task_notification", TaskID: "task-1", Status: "completed", IsBackgrounded: true})
	if delivered != 1 {
		t.Fatalf("notification count = %d, want 1", delivered)
	}
}
