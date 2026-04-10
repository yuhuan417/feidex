package app

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/release"
	"feidex/internal/state"
)

func TestDispatchCardActionRoutesCommonBranches(t *testing.T) {
	origManager := newDaemonManager
	origRelease := newReleaseClient
	origVersion := currentVersion
	origGOARCH := currentGOARCH
	defer func() {
		newDaemonManager = origManager
		newReleaseClient = origRelease
		currentVersion = origVersion
		currentGOARCH = origGOARCH
	}()

	newDaemonManager = func() (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	newReleaseClient = func() releaseClient {
		return &fakeReleaseClient{info: &release.ReleaseInfo{
			Version:        "v9.9.9",
			BinaryURL:      "https://download.test/feidex",
			ExpectedSHA256: "abc123",
			HTMLURL:        "https://example.test/releases/v9.9.9",
		}}
	}
	currentVersion = func() string { return "v0.1.0" }
	currentGOARCH = func() string { return "amd64" }

	a, _, fc := newTestApp(t)
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "alt", Cwd: t.TempDir()})
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-1",
		WorkspaceID:             "default",
		ChatID:                  "chat-1",
		ChatType:                "group",
		OwnerUserID:             "user-1",
		ActiveThreadID:          "thread-1",
		ActiveThreadName:        "Thread 1",
		ActiveThreadPreview:     "preview",
		ActiveThreadWorkspaceID: "default",
		ActiveTurnID:            "turn-1",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-2",
		WorkspaceID:             "default",
		ChatID:                  "chat-1",
		ChatType:                "group",
		OwnerUserID:             "user-1",
		ActiveThreadID:          "thread-2",
		ActiveThreadName:        "Thread 2",
		ActiveThreadPreview:     "preview",
		ActiveThreadWorkspaceID: "default",
	}); err != nil {
		t.Fatalf("UpsertSession(sess-2) error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-1",
		SessionKey:       "sess-1",
		WorkspaceID:      "default",
		ThreadID:         "thread-1",
		TurnID:           "turn-1",
		TriggerMessageID: "trigger-1",
		ChatID:           "chat-1",
		UserID:           "user-1",
		Status:           "running",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	sess := a.store.GetSession("sess-1")
	sess.ActiveSubmissionID = subID
	if err := a.store.UpsertSession(sess); err != nil {
		t.Fatalf("UpsertSession(active submission) error = %v", err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "toggle-1",
		Kind:        "turn_item_card",
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		PayloadJSON: mustJSON(turnItemCardPayload{
			SubmissionID: subID,
			SessionKey:   "sess-1",
			TurnID:       "turn-1",
			ItemType:     "agent_message",
			Title:        "回复",
			Color:        "green",
			SummaryText:  "hello",
		}),
		Status: "pending",
	}); err != nil {
		t.Fatalf("UpsertPending(toggle) error = %v", err)
	}

	models := codexrpc.ModelListResult{
		Data: []codexrpc.ModelListEntry{{
			ID:                     "gpt-5",
			DisplayName:            "GPT-5",
			DefaultReasoningEffort: "medium",
			SupportedReasoningEfforts: []codexrpc.ModelReasoningEffortEntry{
				{ReasoningEffort: "medium"},
				{ReasoningEffort: "high"},
			},
			IsDefault: true,
		}},
	}
	threadName := "Thread 1"
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "model/list":
			*out.(*codexrpc.ModelListResult) = models
		case "thread/read":
			*out.(*codexrpc.ThreadReadResult) = codexrpc.ThreadReadResult{
				Thread: codexrpc.ThreadReadThread{
					ID:   "thread-1",
					Name: &threadName,
					Turns: []codexrpc.ThreadReadTurn{{
						ID:     "turn-1",
						Status: "completed",
						Items: []codexrpc.ThreadReadItem{
							{Type: "userMessage", Content: json.RawMessage(`[{"type":"text","text":"hello"}]`)},
							{Type: "agentMessage", Text: "world"},
						},
					}},
				},
			}
		case "thread/resume":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-2"
			result.Thread.Name = "Thread 2"
			result.Thread.Preview = "preview"
		case "thread/fork":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-fork"
			result.Thread.Name = "Forked Thread"
			result.Thread.Preview = "fork preview"
		}
		return nil
	}

	if err := a.store.UpsertPending(&state.PendingRequest{ID: "input-1", Kind: "tool_request_user_input", OwnerUserID: "user-1", Status: "pending"}); err != nil {
		t.Fatalf("UpsertPending(input) error = %v", err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{ID: "approval-1", Kind: "command", OwnerUserID: "user-1", Status: "pending", RequestIDRaw: `"approval-1"`}); err != nil {
		t.Fatalf("UpsertPending(approval) error = %v", err)
	}
	pathRoot := t.TempDir()
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "path-1",
		Kind:        pathPickerKind,
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(pathPickerPayload{
			Mode:        pathPickerModeDirectory,
			Style:       pathPickerStyleDropdown,
			RootPath:    pathRoot,
			CurrentPath: pathRoot,
		}),
	}); err != nil {
		t.Fatalf("UpsertPending(path picker) error = %v", err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{ID: "upgrade-1", Kind: "upgrade_release", OwnerUserID: "user-1", Status: "pending", PayloadJSON: mustJSON(upgradePendingPayload{
		TargetVersion:  "v9.9.9",
		BinaryPath:     "/tmp/feidex",
		DownloadURL:    "https://download.test/feidex",
		ExpectedSHA256: "abc123",
	})}); err != nil {
		t.Fatalf("UpsertPending(upgrade) error = %v", err)
	}

	cases := []feishu.CardAction{
		{ActionValue: map[string]any{"action": "menu.model", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.reasoning", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.history", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "history.page", "session_key": "sess-1", "page": float64(0)}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "service_tier.set", "session_key": "sess-1", "thread_id": "thread-1", "service_tier": serviceTierFast}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.upgrade", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "quiet.set", "session_key": "sess-1", "enabled": true}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "workspace.use", "session_key": "sess-1", "workspace_id": "alt"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "thread.sandbox.menu", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "thread.policy.menu", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{Name: "turn.item.toggle:toggle-1:expanded", UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.root", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.group.session", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.group.context", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.download", "session_key": "sess-1", "parent_action": "menu.group.context"}, UserID: "user-1", ChatID: "chat-1", MessageID: "msg-1"},
		{ActionValue: map[string]any{"action": "menu.fork", "session_key": "sess-1", "parent_action": "menu.group.context"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.compact", "session_key": "sess-2", "parent_action": "menu.group.context"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.group.model", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.group.system", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.debug", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.debug.logs", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.new", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "model.config.set_model", "session_key": "sess-1", "model_id": "gpt-5"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "model.config.set_effort", "session_key": "sess-1", "reasoning_effort": "high"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.threads", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "menu.interrupt", "session_key": "sess-1", "turn_id": "turn-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "workspace.new", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "workspace.sandbox.menu", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "workspace.policy.menu", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "workspace.sandbox.set", "session_key": "sess-1", "workspace_id": "default", "sandbox_mode": "read-only"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "workspace.policy.set", "session_key": "sess-1", "workspace_id": "default", "approval_policy": "never"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "thread.sandbox.set", "session_key": "sess-1", "thread_id": "thread-1", "sandbox_mode": "read-only"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "thread.policy.set", "session_key": "sess-1", "thread_id": "thread-1", "approval_policy": "never"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "thread.resume", "session_key": "sess-1", "thread_id": "thread-2"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "history.detail", "session_key": "sess-1", "index": float64(0)}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "turn.append", "session_key": "sess-1", "turn_id": "turn-1", "item_id": "item-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "user_input.answer", "request_id": "input-1", "question_id": "mode", "answer": "Fast"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "path_picker.cancel", "request_id": "path-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "upgrade.cancel", "request_id": "upgrade-1", "session_key": "sess-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "approval.command.accept", "request_id": "approval-1"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "pending_form.cancel", "request_id": "missing"}, UserID: "user-1", ChatID: "chat-1"},
		{ActionValue: map[string]any{"action": "elicitation_url.accept", "request_id": "missing"}, UserID: "user-1", ChatID: "chat-1"},
	}

	for _, tc := range cases {
		resp, err := a.dispatchCardAction(&tc)
		if err != nil {
			t.Fatalf("dispatchCardAction(%q) error = %v", tc.Name, err)
		}
		if resp == nil {
			t.Fatalf("dispatchCardAction(%q) returned nil response", tc.Name)
		}
	}
}
