package conversation

import "testing"

func TestActiveOperationLifecycleKeepsForegroundFieldsInSync(t *testing.T) {
	sess := &Session{}
	UpsertActiveOperation(sess, SessionActiveOperation{Kind: OpKindSubmission, SubmissionID: "sub-1", ThreadID: "thread-1"})
	if !HasActiveOperations(sess) || sess.ActiveSubmissionID != "sub-1" || sess.ActiveThreadID != "thread-1" {
		t.Fatalf("upsert did not establish foreground operation: %+v", sess)
	}
	UpsertActiveOperation(sess, SessionActiveOperation{SubmissionID: "sub-1", TurnID: "turn-1"})
	if op := FindActiveOperationByTurn(sess, "turn-1"); op == nil || op.SubmissionID != "sub-1" {
		t.Fatalf("turn binding not merged: %+v", sess.ActiveOperations)
	}
	if !RemoveActiveOperation(sess, "sub-1", "turn-1") || HasActiveOperations(sess) {
		t.Fatalf("remove left active operation: %+v", sess)
	}
}

func TestBackendLineageIsScopedByBackend(t *testing.T) {
	sess := &Session{ActiveThreadID: "codex-thread", ActiveThreadWorkspaceID: "ws", ActiveThreadApprovalPolicy: "never"}
	StoreBackendThread(sess, "codex")
	ClearThreadContext(sess)
	if !RestoreBackendThread(sess, "codex") || sess.ActiveThreadID != "codex-thread" || sess.ActiveThreadApprovalPolicy != "never" {
		t.Fatalf("codex lineage did not restore: %+v", sess)
	}
	if RestoreBackendThread(sess, "claude") {
		t.Fatal("missing backend lineage restored successfully")
	}
}

func TestResumeRequiresThreadAndMatchingWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sess      *Session
		workspace string
		want      bool
	}{
		{"missing session", nil, "ws", false},
		{"missing thread", &Session{ActiveThreadWorkspaceID: "ws"}, "ws", false},
		{"missing workspace", &Session{ActiveThreadID: "thread"}, "ws", false},
		{"different workspace", &Session{ActiveThreadID: "thread", ActiveThreadWorkspaceID: "other"}, "ws", false},
		{"matching workspace", &Session{ActiveThreadID: " thread ", ActiveThreadWorkspaceID: " ws "}, "ws", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanResumeThreadForWorkspace(tc.sess, tc.workspace); got != tc.want {
				t.Fatalf("CanResumeThreadForWorkspace = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWorkspaceSwitchPreservesInFlightLineage(t *testing.T) {
	sess := &Session{WorkspaceID: "old", ActiveThreadID: "thread", ActiveSubmissionID: "sub"}
	SwitchSessionWorkspace(sess, "new")
	if sess.WorkspaceID != "new" || sess.ActiveThreadWorkspaceID != "old" || sess.ActiveThreadID != "thread" {
		t.Fatalf("in-flight lineage lost on workspace switch: %+v", sess)
	}
	ResetActiveOperations(sess)
	SwitchSessionWorkspace(sess, "third")
	if sess.ActiveThreadID != "" || sess.BackendThreads != nil {
		t.Fatalf("idle workspace switch retained old lineage: %+v", sess)
	}
}

func TestThreadDefaultsPreserveExplicitOverrides(t *testing.T) {
	sess := &Session{ActiveThreadApprovalPolicy: " never ", ActiveThreadSandboxMode: " read-only ", ActiveThreadMultiAgentMode: " disabled "}
	if EffectiveApprovalPolicy(sess, "untrusted") != "never" || EffectiveSandboxMode(sess, "workspace-write") != "read-only" || EffectiveMultiAgentMode(sess, "enabled") != "disabled" {
		t.Fatal("workspace defaults overrode thread configuration")
	}
	if EffectiveApprovalPolicy(nil, " untrusted ") != "untrusted" || EffectiveSandboxMode(nil, " read-only ") != "read-only" || EffectiveMultiAgentMode(nil, "") != "explicitRequestOnly" {
		t.Fatal("missing thread did not use conservative defaults")
	}
}
