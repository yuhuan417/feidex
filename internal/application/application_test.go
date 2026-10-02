package application

import (
	"testing"

	"feidex/internal/domain/identity"
)

func TestInputsAndEffectsAreConcreteTransportIndependentValues(t *testing.T) {
	var input Input = MessageReceived{
		Frontend: identity.FrontendID("frontend-a"),
		Chat:     identity.ChatRef{Type: identity.ChatTypeGroup, ID: "chat-a"},
	}
	var effect Effect = SendMessage{
		Frontend: identity.FrontendID("frontend-a"),
		Chat:     identity.ChatRef{Type: identity.ChatTypeGroup, ID: "chat-a"},
		Text:     "ok",
	}
	if input == nil || effect == nil {
		t.Fatal("input/effect marker values must be usable through their contracts")
	}
}
