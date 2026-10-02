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
	message := input.(MessageReceived)
	outbound := effect.(SendMessage)
	if message.Frontend != outbound.Frontend || message.Chat != outbound.Chat || outbound.Text != "ok" {
		t.Fatal("input/effect values must preserve frontend and chat identity")
	}
}
