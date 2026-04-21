package app

import (
	"testing"

	"feidex/internal/state"
)

func TestSessionShouldStartNextSubmissionAsync(t *testing.T) {
	tests := []struct {
		name string
		sess *state.Session
		want bool
	}{
		{
			name: "nil session",
			sess: nil,
			want: false,
		},
		{
			name: "empty session",
			sess: &state.Session{},
			want: false,
		},
		{
			name: "staged images only",
			sess: &state.Session{
				StagedImages: []state.SessionStagedImage{{SourceMessageID: "img-1"}},
			},
			want: false,
		},
		{
			name: "queued submission only",
			sess: &state.Session{
				Queue: []string{"sub-1"},
			},
			want: true,
		},
		{
			name: "active submission blocks async start",
			sess: &state.Session{
				Queue:              []string{"sub-1"},
				ActiveSubmissionID: "sub-running",
			},
			want: false,
		},
		{
			name: "active operations block async start",
			sess: &state.Session{
				Queue: []string{"sub-1"},
				ActiveOperations: []state.SessionActiveOperation{{
					Kind:         sessionOpKindSubmission,
					SubmissionID: "sub-running",
					TurnID:       "turn-1",
				}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionShouldStartNextSubmissionAsync(tt.sess); got != tt.want {
				t.Fatalf("sessionShouldStartNextSubmissionAsync() = %v, want %v", got, tt.want)
			}
		})
	}
}
