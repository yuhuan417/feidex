package app

import (
	"context"
	"path/filepath"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCompleteMenuInterruptRejectsStaleTurnCard(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	a := &App{store: store, cfg: testCodexConfig()}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-1",
		ActiveThreadID: "thread-new",
		ActiveTurnID:   "turn-new",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := newThreadService(a).CompleteMenuInterrupt(nil, "sess-1", "turn-old")
	if err != nil {
		t.Fatalf("completeMenuInterrupt: %v", err)
	}
	if resp == nil || resp.Toast == nil {
		t.Fatal("expected warning toast")
	}
	if resp.Toast.Type != "warning" {
		t.Fatalf("unexpected toast type: %q", resp.Toast.Type)
	}
}

func TestCompleteMenuNewRejectsRunningTurn(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	a := &App{store: store, cfg: testCodexConfig()}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-1",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := newThreadService(a).CompleteMenuNew(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1")
	if err != nil {
		t.Fatalf("completeMenuNew: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("expected warning toast, got %#v", resp)
	}
}

func TestCompleteThreadResumeRejectsRunningTurn(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	a := &App{store: store, cfg: testCodexConfig()}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-1",
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := newThreadService(a).CompleteThreadResume(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1", "thread-2")
	if err != nil {
		t.Fatalf("completeThreadResume: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("expected warning toast, got %#v", resp)
	}
}

func TestCompleteWorkspaceUseRejectsRunningTurn(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	cfg := testCodexConfig()
	cfg.Workspaces = append(cfg.Workspaces, config.Workspace{ID: "alt", Cwd: t.TempDir()})
	a := &App{store: store, cfg: cfg, feishu: feishu.New(cfg.Feishu)}
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-1",
		OwnerUserID:             "u-1",
		ChatID:                  "c-1",
		ChatType:                "p2p",
		WorkspaceID:             "default",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "default",
		ActiveTurnID:            "turn-1",
		ActiveSubmissionID:      "sub-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := newWorkspaceService(a).completeWorkspaceUse(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1", "alt")
	if err != nil {
		t.Fatalf("completeWorkspaceUse: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("expected warning toast, got %#v", resp)
	}
	sess := a.store.GetSession("sess-1")
	if sess == nil {
		t.Fatal("expected session to exist")
	}
	if sess.WorkspaceID != "default" {
		t.Fatalf("workspace = %q, want default", sess.WorkspaceID)
	}
	if sess.ActiveThreadID != "thread-1" || sess.ActiveThreadWorkspaceID != "default" {
		t.Fatalf("expected thread lineage preserved, got %#v", sess)
	}
	if sess.ActiveTurnID != "turn-1" || sess.ActiveSubmissionID != "sub-1" {
		t.Fatalf("expected turn lineage preserved, got %#v", sess)
	}
	if selection := a.store.GetSession(makeWorkspaceSelectionKey(a, "p2p", "c-1", "u-1")); selection != nil {
		t.Fatalf("workspace selection session = %+v, want unchanged", selection)
	}
}

func TestCompleteWorkspaceUseAutoResumesLatestThreadWhenIdle(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "alt", Cwd: t.TempDir()})
	if err := a.store.UpsertSession(&state.Session{
		Key:         "sess-1",
		WorkspaceID: "default",
		OwnerUserID: "u-1",
		ChatID:      "c-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "thread/list":
			*out.(*codexrpc.ThreadListResult) = codexrpc.ThreadListResult{
				Data: []codexrpc.ThreadListEntry{
					{ID: "thread-alt-old", UpdatedAt: 10, Cwd: a.cfg.Workspaces[1].Cwd},
					{ID: "thread-alt-new", UpdatedAt: 20, Name: "Alt Thread", Preview: "Alt Preview", Cwd: a.cfg.Workspaces[1].Cwd},
				},
			}
		case "thread/resume":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-alt-new"
			result.Thread.Name = "Alt Thread"
			result.Thread.Preview = "Alt Preview"
		default:
			t.Fatalf("unexpected method: %s", method)
		}
		return nil
	}

	resp, err := newWorkspaceService(a).completeWorkspaceUse(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1", "alt")
	if err != nil {
		t.Fatalf("completeWorkspaceUse: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("expected success toast, got %#v", resp)
	}
	a.waitAsync()
	sess := a.store.GetSession("sess-1")
	if sess == nil || sess.WorkspaceID != "alt" || sess.ActiveThreadID != "thread-alt-new" || sess.ActiveThreadWorkspaceID != "alt" {
		t.Fatalf("session after workspace resume = %+v", sess)
	}
}

func TestCompleteWorkspaceUseClearsIdleThreadLineageAndPlanMode(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "alt", Cwd: t.TempDir()})
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-1",
		WorkspaceID:             "default",
		OwnerUserID:             "u-1",
		ChatID:                  "c-1",
		ChatType:                "p2p",
		ActiveThreadID:          "thread-old",
		ActiveThreadWorkspaceID: "default",
		ActiveThreadCollaborationMode: &state.SessionCollaborationMode{
			Mode:            "plan",
			Model:           "gpt-5.4",
			ReasoningEffort: "high",
		},
		Status: state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	markSessionThreadLive(a, "sess-1", "thread-old")

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "thread/list":
			*out.(*codexrpc.ThreadListResult) = codexrpc.ThreadListResult{
				Data: []codexrpc.ThreadListEntry{
					{ID: "thread-alt-new", UpdatedAt: 20, Name: "Alt Thread", Preview: "Alt Preview", Cwd: a.cfg.Workspaces[1].Cwd},
				},
			}
		case "thread/resume":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-alt-new"
			result.Thread.Name = "Alt Thread"
			result.Thread.Preview = "Alt Preview"
		default:
			t.Fatalf("unexpected method: %s", method)
		}
		return nil
	}

	resp, err := newWorkspaceService(a).completeWorkspaceUse(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1", "alt")
	if err != nil {
		t.Fatalf("completeWorkspaceUse: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("expected success toast, got %#v", resp)
	}
	a.waitAsync()

	sess := a.store.GetSession("sess-1")
	if sess == nil || sess.WorkspaceID != "alt" || sess.ActiveThreadID != "thread-alt-new" || sess.ActiveThreadWorkspaceID != "alt" {
		t.Fatalf("session after workspace switch = %+v", sess)
	}
	if sess.ActiveThreadCollaborationMode != nil {
		t.Fatalf("plan mode after workspace switch = %+v, want nil", sess.ActiveThreadCollaborationMode)
	}
	if sessionHasLiveThread(a, "sess-1", "thread-old") {
		t.Fatal("expected old live thread binding to be cleared")
	}
	selection := a.store.GetSession(makeWorkspaceSelectionKey(a, "p2p", "c-1", "u-1"))
	if selection == nil || selection.WorkspaceID != "alt" {
		t.Fatalf("workspace selection session = %+v, want alt", selection)
	}
}

func TestCompleteWorkspaceUseStartsThreadWhenWorkspaceHasNone(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "alt", Cwd: t.TempDir()})
	if err := a.store.UpsertSession(&state.Session{
		Key:         "sess-1",
		WorkspaceID: "default",
		OwnerUserID: "u-1",
		ChatID:      "c-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "thread/list":
			*out.(*codexrpc.ThreadListResult) = codexrpc.ThreadListResult{}
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-alt-new"
			result.Thread.Name = "Fresh Thread"
			result.Thread.Preview = "Fresh Preview"
		default:
			t.Fatalf("unexpected method: %s", method)
		}
		return nil
	}

	resp, err := newWorkspaceService(a).completeWorkspaceUse(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1", "alt")
	if err != nil {
		t.Fatalf("completeWorkspaceUse: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("expected success toast, got %#v", resp)
	}
	a.waitAsync()
	sess := a.store.GetSession("sess-1")
	if sess == nil || sess.WorkspaceID != "alt" || sess.ActiveThreadID != "thread-alt-new" || sess.ActiveThreadWorkspaceID != "alt" {
		t.Fatalf("session after workspace start = %+v", sess)
	}
}

func TestCompleteWorkspaceUseFallsBackToStartWhenResumeFails(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "alt", Cwd: t.TempDir()})
	if err := a.store.UpsertSession(&state.Session{
		Key:         "sess-1",
		WorkspaceID: "default",
		OwnerUserID: "u-1",
		ChatID:      "c-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "thread/list":
			*out.(*codexrpc.ThreadListResult) = codexrpc.ThreadListResult{
				Data: []codexrpc.ThreadListEntry{{ID: "thread-alt-old", UpdatedAt: 20, Cwd: a.cfg.Workspaces[1].Cwd}},
			}
			return nil
		case "thread/resume":
			return context.DeadlineExceeded
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-alt-fresh"
			result.Thread.Name = "Fresh Thread"
			result.Thread.Preview = "Fresh Preview"
			return nil
		default:
			t.Fatalf("unexpected method: %s", method)
			return nil
		}
	}

	resp, err := newWorkspaceService(a).completeWorkspaceUse(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1", "alt")
	if err != nil {
		t.Fatalf("completeWorkspaceUse: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("expected success toast, got %#v", resp)
	}
	a.waitAsync()
	sess := a.store.GetSession("sess-1")
	if sess == nil || sess.WorkspaceID != "alt" || sess.ActiveThreadID != "thread-alt-fresh" || sess.ActiveThreadWorkspaceID != "alt" {
		t.Fatalf("session after workspace fallback = %+v", sess)
	}
}

func TestCompleteWorkspaceUseKeepsNewWorkspaceWhenBindingFails(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "alt", Cwd: t.TempDir()})
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-1",
		WorkspaceID:             "default",
		OwnerUserID:             "u-1",
		ChatID:                  "c-1",
		ChatType:                "p2p",
		ActiveThreadID:          "thread-old",
		ActiveThreadWorkspaceID: "default",
		ActiveThreadCollaborationMode: &state.SessionCollaborationMode{
			Mode:  "plan",
			Model: "gpt-5.4",
		},
		Status: state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	markSessionThreadLive(a, "sess-1", "thread-old")

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "thread/list":
			*out.(*codexrpc.ThreadListResult) = codexrpc.ThreadListResult{}
			return nil
		case "thread/start":
			return context.DeadlineExceeded
		default:
			t.Fatalf("unexpected method: %s", method)
			return nil
		}
	}

	resp, err := newWorkspaceService(a).completeWorkspaceUse(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1", "alt")
	if err != nil {
		t.Fatalf("completeWorkspaceUse: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("expected success toast, got %#v", resp)
	}
	a.waitAsync()

	sess := a.store.GetSession("sess-1")
	if sess == nil {
		t.Fatal("expected session to exist")
	}
	if sess.WorkspaceID != "alt" || sess.ActiveThreadID != "" || sess.ActiveThreadWorkspaceID != "" {
		t.Fatalf("session after binding failure = %+v", sess)
	}
	if sess.ActiveThreadCollaborationMode != nil {
		t.Fatalf("plan mode after binding failure = %+v, want nil", sess.ActiveThreadCollaborationMode)
	}
	if sessionHasLiveThread(a, "sess-1", "thread-old") {
		t.Fatal("expected old live thread binding to be cleared")
	}
}

func TestCompleteWorkspaceSandboxSetPersistsConfig(t *testing.T) {
	cfg := testCodexConfig()
	cfg.Workspaces[0].Cwd = t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	a := &App{cfg: cfg, cfgPath: cfgPath, feishu: feishu.New(cfg.Feishu)}

	resp, err := newWorkspaceService(a).completeWorkspaceSandboxSet(&feishu.CardAction{}, "sess-1", "default", "read-only")
	if err != nil {
		t.Fatalf("completeWorkspaceSandboxSet: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("expected success toast, got %#v", resp)
	}
	if got := config.FindWorkspace(a.cfg, "default").SandboxMode; got != "read-only" {
		t.Fatalf("sandbox mode = %q, want read-only", got)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := config.FindWorkspace(loaded, "default").SandboxMode; got != "read-only" {
		t.Fatalf("persisted sandbox mode = %q, want read-only", got)
	}
}

func TestCompleteWorkspacePolicySetPersistsConfig(t *testing.T) {
	cfg := testCodexConfig()
	cfg.Workspaces[0].Cwd = t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	a := &App{cfg: cfg, cfgPath: cfgPath, feishu: feishu.New(cfg.Feishu)}

	resp, err := newWorkspaceService(a).completeWorkspacePolicySet(&feishu.CardAction{}, "sess-1", "default", "never")
	if err != nil {
		t.Fatalf("completeWorkspacePolicySet: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("expected success toast, got %#v", resp)
	}
	if got := config.FindWorkspace(a.cfg, "default").ApprovalPolicy; got != "never" {
		t.Fatalf("approval policy = %q, want never", got)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := config.FindWorkspace(loaded, "default").ApprovalPolicy; got != "never" {
		t.Fatalf("persisted approval policy = %q, want never", got)
	}
}

func TestCompleteWorkspacePolicySetAcceptsUntrusted(t *testing.T) {
	cfg := testCodexConfig()
	cfg.Workspaces[0].Cwd = t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	a := &App{cfg: cfg, cfgPath: cfgPath, feishu: feishu.New(cfg.Feishu)}

	resp, err := newWorkspaceService(a).completeWorkspacePolicySet(&feishu.CardAction{}, "sess-1", "default", "untrusted")
	if err != nil {
		t.Fatalf("completeWorkspacePolicySet: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("expected success toast, got %#v", resp)
	}
	if got := config.FindWorkspace(a.cfg, "default").ApprovalPolicy; got != "untrusted" {
		t.Fatalf("approval policy = %q, want untrusted", got)
	}
}

func TestCompleteThreadSandboxSetUpdatesSessionOnly(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	cfg := testCodexConfig()
	a := &App{store: store, cfg: cfg, feishu: feishu.New(cfg.Feishu)}
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-1",
		WorkspaceID:             "default",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "default",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := newThreadService(a).CompleteThreadSandboxSet(&feishu.CardAction{}, "sess-1", "thread-1", "read-only")
	if err != nil {
		t.Fatalf("completeThreadSandboxSet: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("expected success toast, got %#v", resp)
	}
	sess := a.store.GetSession("sess-1")
	if sess == nil || sess.ActiveThreadSandboxMode != "read-only" {
		t.Fatalf("unexpected thread sandbox override: %#v", sess)
	}
	if got := config.FindWorkspace(a.cfg, "default").SandboxMode; got != "workspace-write" {
		t.Fatalf("workspace sandbox should stay default, got %q", got)
	}
}

func TestCompleteThreadPolicySetUpdatesSessionOnly(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	cfg := testCodexConfig()
	a := &App{store: store, cfg: cfg, feishu: feishu.New(cfg.Feishu)}
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-1",
		WorkspaceID:             "default",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "default",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := newThreadService(a).CompleteThreadPolicySet(&feishu.CardAction{}, "sess-1", "thread-1", "untrusted")
	if err != nil {
		t.Fatalf("completeThreadPolicySet: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("expected success toast, got %#v", resp)
	}
	sess := a.store.GetSession("sess-1")
	if sess == nil || sess.ActiveThreadApprovalPolicy != "untrusted" {
		t.Fatalf("unexpected thread policy override: %#v", sess)
	}
	if got := config.FindWorkspace(a.cfg, "default").ApprovalPolicy; got != "on-request" {
		t.Fatalf("workspace policy should stay default, got %q", got)
	}
}
