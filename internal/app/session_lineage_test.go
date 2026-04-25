package app

import (
	"testing"

	"feidex/internal/app/sessionctx"
	"feidex/internal/config"
	"feidex/internal/state"
)

func TestSwitchSessionWorkspaceClearsIdleThreadContext(t *testing.T) {
	sess := &state.Session{
		WorkspaceID:             "ws-old",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "ws-old",
		ActiveThreadName:        "thread name",
		ActiveThreadPreview:     "thread preview",
		BackendThreads: map[string]state.SessionBackendThread{
			backendCodex: {ThreadID: "thread-1", WorkspaceID: "ws-old"},
		},
	}

	switchSessionWorkspace(sess, "ws-new")

	if sess.WorkspaceID != "ws-new" {
		t.Fatalf("workspace = %q, want ws-new", sess.WorkspaceID)
	}
	if sess.ActiveThreadID != "" || sess.ActiveThreadWorkspaceID != "" {
		t.Fatalf("expected idle workspace switch to clear thread lineage, got %#v", sess)
	}
	if sess.ActiveThreadName != "" || sess.ActiveThreadPreview != "" {
		t.Fatalf("expected idle workspace switch to clear thread labels, got %#v", sess)
	}
	if len(sess.BackendThreads) != 0 {
		t.Fatalf("expected idle workspace switch to clear backend snapshots, got %#v", sess.BackendThreads)
	}
}

func TestSwitchSessionWorkspacePreservesRunningTurnLineage(t *testing.T) {
	sess := &state.Session{
		WorkspaceID:        "ws-old",
		ActiveThreadID:     "thread-1",
		ActiveTurnID:       "turn-1",
		ActiveSubmissionID: "sub-1",
	}

	switchSessionWorkspace(sess, "ws-new")

	if sess.WorkspaceID != "ws-new" {
		t.Fatalf("workspace = %q, want ws-new", sess.WorkspaceID)
	}
	if sess.ActiveThreadID != "thread-1" {
		t.Fatalf("active thread = %q, want thread-1", sess.ActiveThreadID)
	}
	if sess.ActiveThreadWorkspaceID != "ws-old" {
		t.Fatalf("active thread workspace = %q, want ws-old", sess.ActiveThreadWorkspaceID)
	}
	if sess.ActiveTurnID != "turn-1" || sess.ActiveSubmissionID != "sub-1" {
		t.Fatalf("expected running turn lineage preserved, got %#v", sess)
	}
}

func TestSessionCanResumeThreadForSubmissionRequiresMatchingWorkspace(t *testing.T) {
	sess := &state.Session{
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "ws-a",
	}
	if !sessionCanResumeThreadForSubmission(sess, &state.Submission{WorkspaceID: "ws-a"}) {
		t.Fatal("expected matching workspace to allow thread resume")
	}
	if sessionCanResumeThreadForSubmission(sess, &state.Submission{WorkspaceID: "ws-b"}) {
		t.Fatal("expected mismatched workspace to block thread resume")
	}
	sess.ActiveThreadWorkspaceID = ""
	if sessionCanResumeThreadForSubmission(sess, &state.Submission{WorkspaceID: "ws-a"}) {
		t.Fatal("expected missing thread workspace lineage to block resume")
	}
}

func TestSessionLiveThreadMarkers(t *testing.T) {
	a := &App{}
	if sessionHasLiveThread(a, "sess-1", "thread-1") {
		t.Fatal("expected empty live-thread map to return false")
	}
	markSessionThreadLive(a, "sess-1", "thread-1")
	if !sessionHasLiveThread(a, "sess-1", "thread-1") {
		t.Fatal("expected live-thread marker to be stored")
	}
	clearSessionLiveThread(a, "sess-1")
	if sessionHasLiveThread(a, "sess-1", "thread-1") {
		t.Fatal("expected live-thread marker to be cleared")
	}
}

func TestSessionHasInFlightSubmission(t *testing.T) {
	if sessionHasInFlightSubmission(&state.Session{}) {
		t.Fatal("expected empty session to be idle")
	}
	if !sessionHasInFlightSubmission(&state.Session{ActiveSubmissionID: "sub-1"}) {
		t.Fatal("expected active submission to count as in-flight")
	}
	if !sessionHasInFlightSubmission(&state.Session{ActiveTurnID: "turn-1"}) {
		t.Fatal("expected active turn to count as in-flight")
	}
}

func TestEffectiveThreadDefaultsPreferThreadOverride(t *testing.T) {
	ws := &config.Workspace{
		ApprovalPolicy: "on-request",
		SandboxMode:    "workspace-write",
	}
	sess := &state.Session{
		ActiveThreadApprovalPolicy: "untrusted",
		ActiveThreadSandboxMode:    "read-only",
	}
	if got := effectiveThreadApprovalPolicy(sess, ws); got != "untrusted" {
		t.Fatalf("approval policy = %q, want untrusted", got)
	}
	if got := effectiveThreadSandboxMode(sess, ws); got != "read-only" {
		t.Fatalf("sandbox mode = %q, want read-only", got)
	}
}

func TestSessionStoreAndRestoreBackendThread(t *testing.T) {
	sess := &state.Session{
		WorkspaceID:                "ws-codex",
		ActiveThreadID:             "codex-thread-1",
		ActiveThreadWorkspaceID:    "ws-codex",
		ActiveThreadApprovalPolicy: "never",
		ActiveThreadSandboxMode:    "read-only",
		ActiveThreadServiceTier:    serviceTierFast,
		ActiveThreadName:           "Codex Thread",
		ActiveThreadPreview:        "preview",
	}

	sessionStoreBackendThread(sess, backendCodex)
	clearSessionThreadContext(sess)
	sess.WorkspaceID = "ws-claude"

	if !sessionctx.RestoreBackendThread(sess, backendCodex) {
		t.Fatal("expected codex backend thread snapshot to restore")
	}
	if sess.WorkspaceID != "ws-codex" || sess.ActiveThreadID != "codex-thread-1" {
		t.Fatalf("restored session = %+v", sess)
	}
	if sess.ActiveThreadSandboxMode != "read-only" || sess.ActiveThreadApprovalPolicy != "never" || sess.ActiveThreadServiceTier != serviceTierFast {
		t.Fatalf("restored thread defaults = %+v", sess)
	}
}
