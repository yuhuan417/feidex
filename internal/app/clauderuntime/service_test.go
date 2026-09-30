package clauderuntime

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	appapproval "feidex/internal/app/approval"
	appruntime "feidex/internal/app/runtime"
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

func TestSessionRestartReasonCoversModelAndAuxiliaryChanges(t *testing.T) {
	live := &SessionState{
		Model:                  "deepseek-flash[1m]",
		AuxiliarySmallModel:    "haiku",
		AuxiliarySubagentModel: "opus",
	}

	if got := sessionRestartReason(live, false, "deepseek-flash[1m]", "haiku", "opus"); got != "" {
		t.Fatalf("unchanged session restart reason = %q, want reuse", got)
	}
	if got := sessionRestartReason(live, false, " claude-fable-5 ", "haiku", "opus"); got != "primary_model_changed" {
		t.Fatalf("changed primary model restart reason = %q, want primary_model_changed", got)
	}
	if got := sessionRestartReason(live, false, "deepseek-flash[1m]", "sonnet", "opus"); got != "auxiliary_models_changed" {
		t.Fatalf("changed small model restart reason = %q, want auxiliary_models_changed", got)
	}
	if got := sessionRestartReason(live, true, "deepseek-flash[1m]", "haiku", "opus"); got != "process_stopped" {
		t.Fatalf("stopped session restart reason = %q, want process_stopped", got)
	}
	if got := sessionRestartReason(live, false, "", "", ""); got != "primary_model_changed" {
		t.Fatalf("cleared model restart reason = %q, want primary_model_changed", got)
	}
}

// A background agent can ask for permission after the turn that spawned it was
// cleaned up. The request must be delivered to the conversation instead of
// being denied, and it must stay answerable.
func TestHandlePermissionDeliversDetachedCardAfterTurnCleanup(t *testing.T) {
	var (
		mu        sync.Mutex
		gotID     string
		gotTarget InteractionTarget
	)
	svc := NewService(Deps{
		Interactive: InteractiveDeps{
			SendDetachedApprovalCard: func(requestID string, target InteractionTarget, presentation appapproval.Presentation) error {
				mu.Lock()
				gotID, gotTarget = requestID, target
				mu.Unlock()
				return nil
			},
		},
		Lookup: LookupDeps{
			FindSubmissionByTurn: func(string, string) (string, *state.Submission) { return "", nil },
			GetSession: func(string) *state.Session {
				return &state.Session{
					Key:            "session-1",
					ChatID:         "chat-1",
					RootMessageID:  "root-1",
					OwnerUserID:    "user-1",
					ActiveThreadID: "thread-1",
				}
			},
		},
	})
	runtimeState := &SessionState{SessionKey: "session-1", SessionID: "thread-1"}

	done := make(chan *claudecli.PermissionResponse, 1)
	go func() {
		resp, _ := svc.handlePermission(context.Background(), runtimeState, &claudecli.PermissionRequest{
			RequestID: "req-detached",
			ToolName:  "Bash",
		})
		done <- resp
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		delivered := gotID != ""
		mu.Unlock()
		if delivered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("detached approval card was never delivered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	target := gotTarget
	mu.Unlock()
	if !target.Detached() {
		t.Fatalf("target = %#v, want a detached request", target)
	}
	if target.ChatID != "chat-1" || target.TriggerMessageID != "root-1" || target.UserID != "user-1" {
		t.Fatalf("target anchors = %#v, want the session's Feishu anchors", target)
	}

	if err := svc.ResolveApproval("req-detached", appruntime.ClaudeApprovalResolution{Behavior: "allow"}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	select {
	case resp := <-done:
		if resp == nil || resp.Behavior != claudecli.PermissionAllow {
			t.Fatalf("permission response = %#v, want allow", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("permission handler did not resume after the answer")
	}
}

// Without a live turn and without any conversation anchor there is nobody to
// ask, which is the only case that may be denied locally.
func TestHandlePermissionDeniesWithoutAnyDeliveryTarget(t *testing.T) {
	svc := NewService(Deps{
		Lookup: LookupDeps{
			FindSubmissionByTurn: func(string, string) (string, *state.Submission) { return "", nil },
		},
	})
	runtimeState := &SessionState{SessionKey: "session-1", SessionID: "thread-1"}
	resp, err := svc.handlePermission(context.Background(), runtimeState, &claudecli.PermissionRequest{
		RequestID: "req-orphan",
		ToolName:  "Bash",
	})
	if err != nil {
		t.Fatalf("handlePermission() error = %v", err)
	}
	if resp == nil || resp.Behavior != claudecli.PermissionDeny {
		t.Fatalf("permission response = %#v, want deny", resp)
	}
}

func TestResetSessionExpiresInteractionCards(t *testing.T) {
	var (
		gotSession string
		gotIDs     []string
		gotReason  string
	)
	svc := NewService(Deps{
		Interactive: InteractiveDeps{
			ExpireInteractionCards: func(sessionKey string, requestIDs []string, reason string) {
				gotSession, gotIDs, gotReason = sessionKey, requestIDs, reason
			},
		},
	})
	runtimeState := &SessionState{SessionKey: "session-1", SessionID: "thread-1"}
	svc.storePending("req-1", &PendingInteraction{
		Kind:    "command",
		Session: runtimeState,
		RespCh:  make(chan PendingResponse, 1),
	})

	if err := svc.ResetSession("session-1"); err != nil {
		t.Fatalf("ResetSession() error = %v", err)
	}
	if gotSession != "session-1" || len(gotIDs) != 1 || gotIDs[0] != "req-1" || gotReason != "session reset" {
		t.Fatalf("expire call = (%q, %#v, %q), want session-1/[req-1]/session reset", gotSession, gotIDs, gotReason)
	}
	if _, ok := svc.pending["req-1"]; ok {
		t.Fatal("pending interaction should be released on reset")
	}
}

// The event loop only exits when the CLI process is gone, so even a session
// with no active turn must release its pending cards.
func TestCleanupStaleSessionOpsReleasesPendingCards(t *testing.T) {
	var gotIDs []string
	svc := NewService(Deps{
		Interactive: InteractiveDeps{
			ExpireInteractionCards: func(sessionKey string, requestIDs []string, reason string) {
				gotIDs = requestIDs
			},
		},
		Lookup: LookupDeps{
			GetSession:          func(string) *state.Session { return &state.Session{Key: "session-1"} },
			SessionHasActiveOps: func(*state.Session) bool { return false },
		},
	})
	runtimeState := &SessionState{SessionKey: "session-1", SessionID: "thread-1"}
	svc.storePending("req-detached", &PendingInteraction{
		Kind:    "command",
		Session: runtimeState,
		RespCh:  make(chan PendingResponse, 1),
	})

	svc.cleanupStaleSessionOps(runtimeState)

	if len(gotIDs) != 1 || gotIDs[0] != "req-detached" {
		t.Fatalf("expired ids = %#v, want the session's pending request", gotIDs)
	}
	if _, ok := svc.pending["req-detached"]; ok {
		t.Fatal("pending interaction should be released when the event loop exits")
	}
}

// A request the CLI withdrew (turn interrupted while the card was open) must
// release the handler and close out the card instead of leaving it clickable.
func TestHandlePermissionWithdrawnRequestExpiresCard(t *testing.T) {
	var (
		gotIDs    []string
		gotReason string
	)
	svc := NewService(Deps{
		Interactive: InteractiveDeps{
			SendDetachedApprovalCard: func(string, InteractionTarget, appapproval.Presentation) error { return nil },
			ExpireInteractionCards: func(_ string, requestIDs []string, reason string) {
				gotIDs, gotReason = requestIDs, reason
			},
		},
		Lookup: LookupDeps{
			FindSubmissionByTurn: func(string, string) (string, *state.Submission) { return "", nil },
			GetSession:           func(string) *state.Session { return &state.Session{Key: "session-1", ChatID: "chat-1"} },
		},
	})
	runtimeState := &SessionState{SessionKey: "session-1", SessionID: "thread-1"}

	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.handlePermission(ctx, runtimeState, &claudecli.PermissionRequest{
			RequestID: "req-withdrawn",
			ToolName:  "Bash",
		})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		svc.mu.Lock()
		_, registered := svc.pending["req-withdrawn"]
		svc.mu.Unlock()
		if registered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("permission request was never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel(claudecli.ErrControlRequestWithdrawn)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not released by the withdrawal")
	}
	if len(gotIDs) != 1 || gotIDs[0] != "req-withdrawn" || gotReason != "request withdrawn" {
		t.Fatalf("expire call = (%#v, %q), want req-withdrawn/request withdrawn", gotIDs, gotReason)
	}
	if _, ok := svc.pending["req-withdrawn"]; ok {
		t.Fatal("withdrawn interaction should be released")
	}
}

func newSubagentSessionState() *SessionState {
	return &SessionState{
		SessionKey: "session-1",
		SessionID:  "thread-current",
		BackgroundTasks: map[string]*BackgroundTaskState{
			"agent-7": {
				Live: true,
				Target: BackgroundTaskTarget{
					SessionKey:       "session-1",
					WorkspaceID:      "ws-old",
					ChatID:           "chat-old",
					TriggerMessageID: "trigger-old",
					UserID:           "user-old",
					ThreadID:         "thread-old",
					TurnID:           "turn-old",
					TaskID:           "agent-7",
					Description:      "inspect repo",
					IsBackgrounded:   true,
				},
			},
		},
	}
}

// A subagent request must be routed by agent_id to the task that spawned it,
// not to whichever turn happens to be current.
func TestHandlePermissionAttributesSubagentRequestToItsTask(t *testing.T) {
	var (
		mu        sync.Mutex
		gotTarget InteractionTarget
		gotBody   string
		gotID     string
	)
	svc := NewService(Deps{
		Interactive: InteractiveDeps{
			SendDetachedApprovalCard: func(requestID string, target InteractionTarget, presentation appapproval.Presentation) error {
				mu.Lock()
				gotID, gotTarget, gotBody = requestID, target, presentation.Body
				mu.Unlock()
				return nil
			},
		},
		Lookup: LookupDeps{
			// The spawning turn is gone: the task target must still be used.
			FindSubmissionByTurn: func(string, string) (string, *state.Submission) { return "", nil },
			GetSession:           func(string) *state.Session { return &state.Session{Key: "session-1", ChatID: "chat-new"} },
		},
	})
	runtimeState := newSubagentSessionState()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.handlePermission(context.Background(), runtimeState, &claudecli.PermissionRequest{
			RequestID: "req-agent",
			ToolName:  "Write",
			ToolUseID: "call-subagent-1",
			AgentID:   "agent-7",
		})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		delivered := gotID != ""
		mu.Unlock()
		if delivered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subagent approval card was never delivered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	target, body := gotTarget, gotBody
	mu.Unlock()
	if target.ChatID != "chat-old" || target.TriggerMessageID != "trigger-old" || target.TurnID != "turn-old" {
		t.Fatalf("target = %#v, want the spawning task's conversation anchors", target)
	}
	if !target.Detached() {
		t.Fatalf("target = %#v, want detached because the spawning turn is gone", target)
	}
	if !strings.Contains(body, "后台 Agent「inspect repo」") {
		t.Fatalf("card body = %q, want the source background agent", body)
	}

	if err := svc.ResolveApproval("req-agent", appruntime.ClaudeApprovalResolution{Behavior: "allow"}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	<-done
}

// A foreground subagent's request stays bound to the submission of the turn
// that spawned it, even when that is not the current turn.
func TestHandlePermissionBindsForegroundSubagentToSpawningTurn(t *testing.T) {
	var (
		mu           sync.Mutex
		gotSub       *state.Submission
		gotSession   string
		detachedUsed bool
	)
	svc := NewService(Deps{
		Interactive: InteractiveDeps{
			SendClaudeApprovalCard: func(requestID, sessionKey string, sub *state.Submission, presentation appapproval.Presentation) error {
				mu.Lock()
				gotSession, gotSub = sessionKey, sub
				mu.Unlock()
				return nil
			},
			SendDetachedApprovalCard: func(string, InteractionTarget, appapproval.Presentation) error {
				mu.Lock()
				detachedUsed = true
				mu.Unlock()
				return nil
			},
		},
		Lookup: LookupDeps{
			FindSubmissionByTurn: func(threadID, turnID string) (string, *state.Submission) {
				if threadID == "thread-old" && turnID == "turn-old" {
					return "session-1", &state.Submission{
						ID: "sub-old", SessionKey: "session-1", WorkspaceID: "ws-old",
						ChatID: "chat-old", TriggerMessageID: "trigger-old", UserID: "user-old",
						ThreadID: "thread-old", TurnID: "turn-old",
					}
				}
				return "", nil
			},
		},
	})
	runtimeState := newSubagentSessionState()
	runtimeState.SessionID = "thread-current"

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.handlePermission(context.Background(), runtimeState, &claudecli.PermissionRequest{
			RequestID: "req-agent-fg",
			ToolName:  "Write",
			AgentID:   "agent-7",
		})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		bound := gotSub != nil
		mu.Unlock()
		if bound {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("approval card was never delivered to the spawning submission")
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	sub, sessionKey, detached := gotSub, gotSession, detachedUsed
	mu.Unlock()
	if sub == nil || sub.ID != "sub-old" || sessionKey != "session-1" {
		t.Fatalf("bound delivery = (%q, %#v), want the spawning turn's submission", sessionKey, sub)
	}
	if detached {
		t.Fatal("foreground subagent request must not be delivered detached")
	}

	if err := svc.ResolveApproval("req-agent-fg", appruntime.ClaudeApprovalResolution{Behavior: "allow"}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	<-done
}

// A task registered without a live turn (a resumed subagent is always
// registered in the background) must still capture the session anchors, so a
// much later permission request can be attributed to it.
func TestBackgroundTaskRegisteredWithoutTurnKeepsSessionAnchors(t *testing.T) {
	var (
		mu        sync.Mutex
		gotTarget InteractionTarget
		gotID     string
	)
	svc := NewService(Deps{
		Interactive: InteractiveDeps{
			SendDetachedApprovalCard: func(requestID string, target InteractionTarget, presentation appapproval.Presentation) error {
				mu.Lock()
				gotID, gotTarget = requestID, target
				mu.Unlock()
				return nil
			},
		},
		Lookup: LookupDeps{
			// The resumed task has no submission of its own.
			FindSubmissionByTurn: func(string, string) (string, *state.Submission) { return "", nil },
			GetSession: func(string) *state.Session {
				return &state.Session{
					Key:            "session-1",
					WorkspaceID:    "ws-1",
					ChatID:         "chat-1",
					RootMessageID:  "root-1",
					OwnerUserID:    "user-1",
					ActiveThreadID: "thread-1",
					ActiveTurnID:   "turn-current",
				}
			},
		},
	})
	runtimeState := &SessionState{SessionKey: "session-1", SessionID: "thread-1"}

	// Registered with no current turn at all.
	svc.HandleBackgroundTaskEvent(runtimeState, claudecli.BackgroundTaskEvent{
		Subtype:        "task_started",
		TaskID:         "agent-resumed",
		IsBackgrounded: true,
		Description:    "resumed task",
	})
	runtimeState.Mu.Lock()
	task := runtimeState.BackgroundTasks["agent-resumed"]
	runtimeState.Mu.Unlock()
	if task == nil {
		t.Fatal("task_started without a live turn was dropped")
	}
	if task.Target.TurnID != "" {
		t.Fatalf("target turn = %q, want empty for a turn-less task", task.Target.TurnID)
	}
	if task.Target.ChatID != "chat-1" || task.Target.TriggerMessageID != "root-1" || task.Target.UserID != "user-1" {
		t.Fatalf("target = %#v, want the session's Feishu anchors", task.Target)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.handlePermission(context.Background(), runtimeState, &claudecli.PermissionRequest{
			RequestID: "req-resumed",
			ToolName:  "Write",
			AgentID:   "agent-resumed",
		})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		delivered := gotID != ""
		mu.Unlock()
		if delivered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("permission request from the resumed task was never delivered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	target := gotTarget
	mu.Unlock()
	if !target.Detached() || target.ChatID != "chat-1" || target.TriggerMessageID != "root-1" {
		t.Fatalf("target = %#v, want a detached delivery to the session anchors", target)
	}
	if err := svc.ResolveApproval("req-resumed", appruntime.ClaudeApprovalResolution{Behavior: "allow"}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	<-done
}
