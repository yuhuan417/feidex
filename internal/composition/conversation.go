package composition

import (
	"context"
	"feidex/internal/application"
	conversationapp "feidex/internal/application/conversation"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/runtime"
)

type ConversationRepository struct {
	Repository conversationapp.Repository
	Runner     runtime.EffectRunner
	Frontend   identity.FrontendID
	Context    context.Context
}

func (r ConversationRepository) Session(key string) *conversation.Session {
	return r.Repository.Session(key)
}
func (r ConversationRepository) SaveSession(session *conversation.Session) error {
	return r.Runner.Run(r.Context, []application.Effect{application.SaveState{Frontend: r.Frontend, Session: session}})
}
