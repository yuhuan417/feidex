package runtime

import "testing"

func TestFrontendOwnersAreFrontendScoped(t *testing.T) {
	a := NewFrontendOwner()
	b := NewFrontendOwner()
	if a == nil || b == nil || a == b {
		t.Fatal("frontend owners must be distinct")
	}
	if a.SessionActors == b.SessionActors || a.LiveThreads == b.LiveThreads || a.AutoRetries == b.AutoRetries || a.CodexRecovery == b.CodexRecovery || a.SubmissionStarts == b.SubmissionStarts || a.EffectDeduper == b.EffectDeduper {
		t.Fatal("frontend runtime state must not be shared")
	}
}
