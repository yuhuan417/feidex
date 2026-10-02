package application

import (
	"context"
	"feidex/internal/domain/identity"
	"fmt"
)

// Result contains the immediate input response and post-transition effects.
// Card callbacks return Response immediately; slow work is an asynchronous
// effect admitted and drained by the frontend runtime.
type Result struct {
	Response any
	Effects  []Effect
}

type Handler[T Input] func(context.Context, T) (Result, error)

// Dispatcher is scoped to one frontend. Transport envelopes never enter its
// handlers, and it rejects inputs belonging to another runtime.
type Dispatcher struct {
	Frontend identity.FrontendID
	Message  Handler[MessageReceived]
	Card     Handler[CardActionReceived]
	Backend  Handler[BackendEventReceived]
	Recall   Handler[MessageRecalled]
	Reaction Handler[MessageReacted]
	Retry    Handler[RetryTimerFired]
}

func (d Dispatcher) Dispatch(ctx context.Context, input Input) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var frontend identity.FrontendID
	switch event := input.(type) {
	case MessageReceived:
		frontend = event.Frontend
	case CardActionReceived:
		frontend = event.Frontend
	case BackendEventReceived:
		frontend = event.Frontend
	case MessageRecalled:
		frontend = event.Frontend
	case MessageReacted:
		frontend = event.Frontend
	case RetryTimerFired:
		frontend = event.Frontend
	default:
		return Result{}, fmt.Errorf("unsupported application input %T", input)
	}
	if frontend.Normalize() != d.Frontend.Normalize() {
		return Result{}, fmt.Errorf("input frontend %q does not match runtime %q", frontend, d.Frontend)
	}
	switch event := input.(type) {
	case MessageReceived:
		if d.Message != nil {
			return d.Message(ctx, event)
		}
	case CardActionReceived:
		if d.Card != nil {
			return d.Card(ctx, event)
		}
	case BackendEventReceived:
		if d.Backend != nil {
			return d.Backend(ctx, event)
		}
	case MessageRecalled:
		if d.Recall != nil {
			return d.Recall(ctx, event)
		}
	case RetryTimerFired:
		if d.Retry != nil {
			return d.Retry(ctx, event)
		}
	case MessageReacted:
		if d.Reaction != nil {
			return d.Reaction(ctx, event)
		}
	}
	return Result{}, fmt.Errorf("application handler unavailable for %T", input)
}
