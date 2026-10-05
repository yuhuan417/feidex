package feishuapp

import (
	domainbackend "feidex/internal/domain/backend"
	domainsubmission "feidex/internal/domain/submission"

	appservicetiercmd "feidex/internal/adapter/feishu/servicetier"
	"feidex/internal/application/announcement"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"testing"
)

func testAppLiveThreadMarker(a *Frontend) liveThreadMarker {
	if a == nil {
		return liveThreadMarker{}
	}
	owner := runtimeViewOf(a.runtimeOwner).ensureRuntimeOwner()
	var announcement announcement.Query
	if a.bindings != nil {
		announcement = a.bindings.AnnouncementQuery
	}
	return liveThreadMarker{
		tracker: owner.LiveThreads, state: a.State(), announcement: announcement, refreshes: owner.Announcements,
	}
}

func markSessionThreadLive(a *Frontend, sessionKey, threadID string) {
	testAppLiveThreadMarker(a).MarkSessionThreadLive(sessionKey, threadID)
}

func sessionHasLiveThread(a *Frontend, sessionKey, threadID string) bool {
	if a == nil {
		return false
	}
	return runtimeViewOf(a.runtimeOwner).ensureRuntimeOwner().LiveThreads.Has(sessionKey, threadID)
}

func clearSessionLiveThread(a *Frontend, sessionKey string) {
	if a != nil {
		runtimeViewOf(a.runtimeOwner).ensureRuntimeOwner().LiveThreads.Clear(sessionKey)
	}
}

func TestSwitchSessionWorkspaceClearsIdleThreadContext(t *testing.T) {
	sess := &conversation.Session{
		WorkspaceID:             "ws-old",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "ws-old",
		ActiveThreadName:        "thread name",
		ActiveThreadPreview:     "thread preview",
		BackendThreads: map[string]conversation.SessionBackendThread{
			domainbackend.BackendCodex: {ThreadID: "thread-1", WorkspaceID: "ws-old"},
		},
	}

	conversation.SwitchSessionWorkspace(sess, "ws-new")

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
	sess := &conversation.Session{
		WorkspaceID:        "ws-old",
		ActiveThreadID:     "thread-1",
		ActiveTurnID:       "turn-1",
		ActiveSubmissionID: "sub-1",
	}

	conversation.SwitchSessionWorkspace(sess, "ws-new")

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
	sess := &conversation.Session{
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "ws-a",
	}
	if !sessionCanResumeThreadForSubmission(sess, &domainsubmission.Submission{WorkspaceID: "ws-a"}) {
		t.Fatal("expected matching workspace to allow thread resume")
	}
	if sessionCanResumeThreadForSubmission(sess, &domainsubmission.Submission{WorkspaceID: "ws-b"}) {
		t.Fatal("expected mismatched workspace to block thread resume")
	}
	sess.ActiveThreadWorkspaceID = ""
	if sessionCanResumeThreadForSubmission(sess, &domainsubmission.Submission{WorkspaceID: "ws-a"}) {
		t.Fatal("expected missing thread workspace lineage to block resume")
	}
}

func TestSessionLiveThreadMarkers(t *testing.T) {
	a := prepareTestApp(&Frontend{})
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
	if conversation.HasInFlightSubmission(&conversation.Session{}) {
		t.Fatal("expected empty session to be idle")
	}
	if !conversation.HasInFlightSubmission(&conversation.Session{ActiveSubmissionID: "sub-1"}) {
		t.Fatal("expected active submission to count as in-flight")
	}
	if !conversation.HasInFlightSubmission(&conversation.Session{ActiveTurnID: "turn-1"}) {
		t.Fatal("expected active turn to count as in-flight")
	}
}

func TestEffectiveThreadDefaultsPreferThreadOverride(t *testing.T) {
	ws := &config.Workspace{
		ApprovalPolicy: "on-request",
		SandboxMode:    "workspace-write",
	}
	sess := &conversation.Session{
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
	sess := &conversation.Session{
		WorkspaceID:                "ws-codex",
		ActiveThreadID:             "codex-thread-1",
		ActiveThreadWorkspaceID:    "ws-codex",
		ActiveThreadApprovalPolicy: "never",
		ActiveThreadSandboxMode:    "read-only",
		ActiveThreadServiceTier:    appservicetiercmd.ServiceTierFast,
		ActiveThreadCollaborationMode: &conversation.SessionCollaborationMode{
			Mode:            "plan",
			Model:           "gpt-5.4",
			ReasoningEffort: "medium",
		},
		ActiveThreadName:    "Codex Thread",
		ActiveThreadPreview: "preview",
	}

	conversation.StoreBackendThread(sess, domainbackend.BackendCodex)
	conversation.ClearThreadContext(sess)
	sess.WorkspaceID = "ws-claude"

	if !conversation.RestoreBackendThread(sess, domainbackend.BackendCodex) {
		t.Fatal("expected codex backend thread snapshot to restore")
	}
	if sess.WorkspaceID != "ws-codex" || sess.ActiveThreadID != "codex-thread-1" {
		t.Fatalf("restored session = %+v", sess)
	}
	if sess.ActiveThreadSandboxMode != "read-only" || sess.ActiveThreadApprovalPolicy != "never" || sess.ActiveThreadServiceTier != appservicetiercmd.ServiceTierFast {
		t.Fatalf("restored thread defaults = %+v", sess)
	}
	if sess.ActiveThreadCollaborationMode == nil || sess.ActiveThreadCollaborationMode.Mode != "plan" || sess.ActiveThreadCollaborationMode.Model != "gpt-5.4" {
		t.Fatalf("restored collaboration mode = %+v", sess.ActiveThreadCollaborationMode)
	}
}
