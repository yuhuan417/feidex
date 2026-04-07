package app

import (
	"path/filepath"
	"testing"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCompleteMenuInterruptRejectsStaleTurnCard(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	a := &App{store: store}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-1",
		ActiveThreadID: "thread-new",
		ActiveTurnID:   "turn-new",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := a.completeMenuInterrupt(nil, "sess-1", "turn-old")
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

	a := &App{store: store}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-1",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := a.completeMenuNew(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1")
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

	a := &App{store: store, cfg: config.Default()}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-1",
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
		ActiveTurnID:   "turn-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := a.completeThreadResume(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1", "thread-2")
	if err != nil {
		t.Fatalf("completeThreadResume: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "warning" {
		t.Fatalf("expected warning toast, got %#v", resp)
	}
}

func TestCompleteWorkspaceUsePreservesRunningTurnLineage(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	cfg := config.Default()
	cfg.Workspaces = append(cfg.Workspaces, config.Workspace{ID: "alt", Cwd: t.TempDir()})
	a := &App{store: store, cfg: cfg}
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-1",
		WorkspaceID:             "default",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "default",
		ActiveTurnID:            "turn-1",
		ActiveSubmissionID:      "sub-1",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := a.completeWorkspaceUse(&feishu.CardAction{UserID: "u-1", ChatID: "c-1"}, "sess-1", "alt")
	if err != nil {
		t.Fatalf("completeWorkspaceUse: %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("expected success toast, got %#v", resp)
	}
	sess := a.store.GetSession("sess-1")
	if sess == nil {
		t.Fatal("expected session to exist")
	}
	if sess.WorkspaceID != "alt" {
		t.Fatalf("workspace = %q, want alt", sess.WorkspaceID)
	}
	if sess.ActiveThreadID != "thread-1" || sess.ActiveThreadWorkspaceID != "default" {
		t.Fatalf("expected thread lineage preserved, got %#v", sess)
	}
	if sess.ActiveTurnID != "turn-1" || sess.ActiveSubmissionID != "sub-1" {
		t.Fatalf("expected turn lineage preserved, got %#v", sess)
	}
}

func TestParseTurnItemToggleName(t *testing.T) {
	requestID, expanded, ok := parseTurnItemToggleName("turn.item.toggle:req-123:expanded")
	if !ok {
		t.Fatal("expected toggle name to parse")
	}
	if requestID != "req-123" {
		t.Fatalf("unexpected request id: %q", requestID)
	}
	if !expanded {
		t.Fatal("expected expanded=true")
	}
}

func TestCompleteWorkspaceSandboxSetPersistsConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	a := &App{cfg: cfg, cfgPath: cfgPath, feishu: feishu.New(cfg.Feishu)}

	resp, err := a.completeWorkspaceSandboxSet(&feishu.CardAction{}, "sess-1", "default", "read-only")
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
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	a := &App{cfg: cfg, cfgPath: cfgPath, feishu: feishu.New(cfg.Feishu)}

	resp, err := a.completeWorkspacePolicySet(&feishu.CardAction{}, "sess-1", "default", "never")
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
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	a := &App{cfg: cfg, cfgPath: cfgPath, feishu: feishu.New(cfg.Feishu)}

	resp, err := a.completeWorkspacePolicySet(&feishu.CardAction{}, "sess-1", "default", "untrusted")
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
	cfg := config.Default()
	a := &App{store: store, cfg: cfg, feishu: feishu.New(cfg.Feishu)}
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-1",
		WorkspaceID:             "default",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "default",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := a.completeThreadSandboxSet(&feishu.CardAction{}, "sess-1", "thread-1", "read-only")
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
	cfg := config.Default()
	a := &App{store: store, cfg: cfg, feishu: feishu.New(cfg.Feishu)}
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-1",
		WorkspaceID:             "default",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "default",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	resp, err := a.completeThreadPolicySet(&feishu.CardAction{}, "sess-1", "thread-1", "untrusted")
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
