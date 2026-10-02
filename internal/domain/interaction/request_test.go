package interaction

import "testing"

func TestReplyAndResolutionAreSeparateTransitions(t *testing.T) {
	req := Request{ID: "r1", Kind: "command", Status: "pending", ThreadID: "thread", TurnID: "turn"}
	replied := req.ReplyAccepted()
	if replied.Status != "replied" || !IsPendingRequestOpen(replied.Status) {
		t.Fatalf("reply transition = %+v", replied)
	}
	resolved, changed := replied.Resolved()
	if !changed || resolved.Status != "resolved" || IsPendingRequestOpen(resolved.Status) {
		t.Fatalf("resolution transition = %+v, changed=%v", resolved, changed)
	}
	if _, changed = resolved.Resolved(); changed {
		t.Fatal("duplicate resolution changed a terminal request")
	}
}

func TestBlocksResumeMatchesThreadOrTurnAndExcludesCurrentRequest(t *testing.T) {
	req := Request{ID: "r1", Kind: "permissions", Status: "replied", ThreadID: "thread", TurnID: "turn"}
	for _, tc := range []struct {
		thread, turn, exclude string
		want                  bool
	}{
		{"thread", "", "", true},
		{"", "turn", "", true},
		{"other", "other", "", false},
		{"thread", "turn", "r1", false},
	} {
		if got := req.BlocksResume(tc.thread, tc.turn, tc.exclude); got != tc.want {
			t.Errorf("BlocksResume(%q,%q,%q) = %v, want %v", tc.thread, tc.turn, tc.exclude, got, tc.want)
		}
	}
}
