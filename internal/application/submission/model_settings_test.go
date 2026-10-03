package submission

import (
	"testing"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/modelconfig"
	domain "feidex/internal/domain/submission"
)

type invalidModelSettings struct{}

func (invalidModelSettings) SubmissionSnapshot(string, *conversation.Session, *domain.Submission) modelconfig.Snapshot {
	return modelconfig.Snapshot{Model: "stale"}
}

func TestMissingOrInvalidModelSnapshotCannotUseStaleSubmissionConfig(t *testing.T) {
	sub := &domain.Submission{ModelConfig: modelconfig.Snapshot{Valid: true, Model: "stale"}}
	for _, settings := range []ModelSettings{nil, invalidModelSettings{}} {
		service := SubmissionQueueService{Deps: Dependencies{ModelSettings: settings}}
		if snapshot, err := service.modelSnapshot(&conversation.Session{}, sub); err == nil || snapshot.Valid {
			t.Fatalf("missing/invalid source fell back to old config: %+v, %v", snapshot, err)
		}
	}
}
