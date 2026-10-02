package submission

import (
	"testing"

	"feidex/internal/domain/modelconfig"
)

func TestAcknowledgedTurnRetainsSnapshotAndBinding(t *testing.T) {
	snapshot := modelconfig.Snapshot{Model: "group-model"}
	s := &Submission{ModelConfig: snapshot, TurnID: "turn-1", TriggerMessageID: "root", Status: SubmissionStatusQueued.String()}
	s.MarkRunning(" thread-1 ", "")
	if s.ThreadID != "thread-1" || s.TurnID != "turn-1" || s.ModelConfig != snapshot || s.Status != SubmissionStatusRunning.String() {
		t.Fatalf("acknowledgment replaced turn input/config: %+v", s)
	}
	s.SetStatus(SubmissionStatusWaitingApproval.String())
	if s.Finalized {
		t.Fatal("waiting approval finalized submission")
	}
	s.SetStatus(SubmissionStatusRunning.String())
	s.Finalize(SubmissionStatusCompleted.String())
	if !s.Finalized || s.Status != SubmissionStatusCompleted.String() || s.TriggerMessageID != "root" || s.ModelConfig != snapshot {
		t.Fatalf("completion lost source/config: %+v", s)
	}
}
