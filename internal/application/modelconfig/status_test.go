package modelconfig

import (
	"testing"

	domain "feidex/internal/domain/modelconfig"
)

func TestSessionStatusReportsConfirmedTurnAndPendingSettings(t *testing.T) {
	desired := domain.Snapshot{Valid: true, Backend: "codex", Model: "new", PlanModel: "new-plan", PlanEffort: "high", CollaborationMode: "plan"}
	applied := domain.Snapshot{Valid: true, Backend: "codex", Model: "old", PlanModel: "old-plan", PlanEffort: "low", CollaborationMode: "plan"}
	status := SessionStatus("codex", "thread", desired, applied, "application failed")
	if !status.HasApplied || !status.Pending || status.NextModel != "new-plan" || status.AppliedModel != "old-plan" || status.NextEffort != "high" || status.AppliedEffort != "low" || status.Error != "application failed" {
		t.Fatalf("status = %+v", status)
	}
	status = SessionStatus("codex", "thread", desired, desired, "")
	if !status.HasApplied || status.Pending {
		t.Fatalf("acknowledged settings still pending: %+v", status)
	}
}

func TestSessionStatusDoesNotClaimUnconfirmedApplication(t *testing.T) {
	desired := domain.Snapshot{Valid: true, Backend: "codex", Model: "group-model"}
	for _, tc := range []struct {
		thread  string
		applied domain.Snapshot
	}{
		{"", desired},
		{"thread", domain.Snapshot{Backend: "codex", Model: "group-model"}},
		{"thread", domain.Snapshot{Valid: true, Backend: "claude", Model: "other-backend"}},
	} {
		status := SessionStatus("codex", tc.thread, desired, tc.applied, "")
		if status.HasApplied || status.AppliedModel != "" || status.NextModel != "group-model" {
			t.Fatalf("unconfirmed application shown: %+v", status)
		}
	}
}
