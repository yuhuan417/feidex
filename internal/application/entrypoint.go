package application

import (
	"context"

	"feidex/internal/domain/identity"
)

// Handlers are the application-owned entrypoint capabilities. Adapters bind
// transport-specific decoding to these handlers in the composition root; the
// dispatcher itself owns the input-family routing contract.
type Handlers struct {
	Message  Handler[MessageReceived]
	Card     Handler[CardActionReceived]
	Backend  Handler[BackendEventReceived]
	Recall   Handler[MessageRecalled]
	Reaction Handler[MessageReacted]
	Retry    Handler[RetryTimerFired]
}

func NewDispatcher(frontend identity.FrontendID, handlers Handlers) Dispatcher {
	return Dispatcher{
		Frontend: frontend,
		Message:  handlers.Message,
		Card:     handlers.Card,
		Backend:  handlers.Backend,
		Recall:   handlers.Recall,
		Reaction: handlers.Reaction,
		Retry:    handlers.Retry,
	}
}

// Ensure the constructor keeps the handler signatures tied to the dispatcher
// contract when this package evolves.
var _ Handler[MessageReceived] = func(context.Context, MessageReceived) (Result, error) { return Result{}, nil }
