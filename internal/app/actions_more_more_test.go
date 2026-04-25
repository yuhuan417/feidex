package app

import (
	"context"
	"os"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/release"
	"feidex/internal/state"
)

func TestActionHelperBranches(t *testing.T) {
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

	newDaemonManager = func(string) (daemon.Manager, error) {
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
		ActiveThreadWorkspaceID: "default",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	threadName := "Thread 1"
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "model/list":
			*out.(*codexrpc.ModelListResult) = codexrpc.ModelListResult{
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
		case "thread/read":
			*out.(*codexrpc.ThreadReadResult) = codexrpc.ThreadReadResult{
				Thread: codexrpc.ThreadReadThread{
					ID:   "thread-1",
					Name: &threadName,
					Turns: []codexrpc.ThreadReadTurn{{
						ID:     "turn-1",
						Status: "completed",
						Items: []codexrpc.ThreadReadItem{
							{Type: "userMessage", Content: []byte(`[{"type":"text","text":"hello"}]`)},
							{Type: "agentMessage", Text: "world"},
						},
					}},
				},
			}
		}
		return nil
	}

	for _, actionName := range []string{"menu.root", "menu.tools", "menu.thread", "menu.group.model", "menu.group.system"} {
		if _, ok := newMenuActionService(a).renderMenuNodeCard(actionName, "sess-1"); !ok {
			t.Fatalf("renderMenuNodeCard(%q) should succeed", actionName)
		}
	}
	if _, ok := newMenuActionService(a).renderMenuNodeCard("missing", "sess-1"); ok {
		t.Fatal("renderMenuNodeCard(missing) should fail")
	}

	if resp, err := newMenuActionService(a).completeHistoryPage(&feishu.CardAction{}, "sess-1", 0); err != nil || resp.Card == nil {
		t.Fatalf("completeHistoryPage() = %#v, %v", resp, err)
	}

	if resp, err := newMenuActionService(a).completeServiceTierSet(&feishu.CardAction{}, "sess-1", "thread-1", serviceTierFast); err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeServiceTierSet() = %#v, %v", resp, err)
	}
	if resp, err := newMenuActionService(a).completeMenuCompact(&feishu.CardAction{ActionValue: map[string]any{"parent_action": "menu.tools"}}, "sess-1"); err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeMenuCompact() = %#v, %v", resp, err)
	}
	if resp, err := newMenuActionService(a).completeMenuUpgrade(&feishu.CardAction{UserID: "user-1", ActionValue: map[string]any{"session_key": "sess-1"}}); err != nil || resp.Toast == nil || resp.Toast.Type != "info" {
		t.Fatalf("completeMenuUpgrade() = %#v, %v", resp, err)
	}

	if resp, err := newThreadService(a).completeMenuInterrupt(&feishu.CardAction{ActionValue: map[string]any{"parent_action": "menu.root"}}, "sess-1", "turn-1"); err != nil || resp.Toast == nil {
		t.Fatalf("completeMenuInterrupt() = %#v, %v", resp, err)
	}

	cfgPath := a.cfgPath
	if _, err := updateWorkspaceDefaults(a, "default", func(w *config.Workspace) { w.Name = "Renamed" }); err != nil {
		t.Fatalf("updateWorkspaceDefaults() error = %v", err)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(config) error = %v", err)
	}
	if config.FindWorkspace(loaded, "default").Name != "Renamed" {
		t.Fatalf("workspace name not persisted: %+v", config.FindWorkspace(loaded, "default"))
	}
}
