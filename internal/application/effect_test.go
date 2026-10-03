package application

import (
	"testing"

	"feidex/internal/application/backendops"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/submission"
)

func TestEffectIdentityUsesStableDomainIdentifiers(t *testing.T) {
	start := StartTurn{
		Frontend:   identity.FrontendID("front-a"),
		SessionKey: identity.SessionKey("session-a"),
		Request:    backendops.StartTurnRequest{Submission: &submission.Submission{ID: "submission-a"}},
	}
	if got := EffectIdentity(start); got == "" || got != EffectIdentity(start) {
		t.Fatalf("start identity = %q, must be stable", got)
	}
	first := EnqueueInput{Frontend: identity.FrontendID("front-a"), SessionKey: "session-a", Message: InboundMessage{MessageID: "message-a"}}
	second := first
	second.Message.Text = "updated payload"
	if EffectIdentity(first) != EffectIdentity(second) {
		t.Fatal("retrying the same inbound message must retain one enqueue identity")
	}
	if EffectIdentity(ResolveBackendRequest{Frontend: identity.FrontendID("front-a"), Backend: "codex", Response: backendops.Response{Token: []byte(`42`)}}) == "" {
		t.Fatal("backend request token must produce an idempotency identity")
	}
}

func TestEffectIdentityKeepsDistinctRepliesSeparate(t *testing.T) {
	first := SendMessage{Frontend: identity.FrontendID("front-a"), ReplyMessageID: "message-a", Text: "first"}
	second := first
	second.Text = "second"
	if EffectIdentity(first) == EffectIdentity(second) {
		t.Fatal("different reply bodies must not share an idempotency identity")
	}
}
