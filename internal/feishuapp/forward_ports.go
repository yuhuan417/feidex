package feishuapp

import (
	"context"
	"feidex/internal/application"
	"feidex/internal/application/inbound"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
)

type forwardGateway interface {
	ResolveMergeForward(context.Context, string, []string) (string, []feishu.Attachment, error)
}

type forwardGatewayAdapter struct{ client forwardGateway }

func ForwardGateway(client forwardGateway) inbound.ForwardGateway {
	return forwardGatewayAdapter{client: client}
}

type forwardTaskAdapter struct {
	lifecycle *frontendruntime.FrontendRuntime
	runner    func(func())
}

func ForwardTasks(lifecycle *frontendruntime.FrontendRuntime, runner func(func())) inbound.ForwardTasks {
	return forwardTaskAdapter{lifecycle: lifecycle, runner: runner}
}

func ForwardFailure(contextFn func() context.Context, frontendID string, runner frontendruntime.EffectRunner) func(*application.InboundMessage, error) {
	return func(msg *application.InboundMessage, err error) {
		_ = replyErrorWith(contextFn, identity.FrontendID(frontendID), runner, msg, err)
	}
}

// ForwardProcessor takes the inbound entry point as a value rather than
// reaching for a.bindings.Inbound, so composition can wire the two services
// together after both exist instead of them holding each other.
func ForwardProcessor(actors *frontendruntime.SessionActors, sessionKey func(*application.InboundMessage) string, process func(*application.InboundMessage) error) func(*application.InboundMessage) error {
	return func(msg *application.InboundMessage) error {
		var err error
		runSessionOnActor(actors, sessionKey(msg), func() { err = process(msg) })
		return err
	}
}
func (p forwardGatewayAdapter) ResolveForward(ctx context.Context, id string, ids []string) (string, []application.Attachment, error) {
	return p.client.ResolveMergeForward(ctx, id, ids)
}

func (p forwardTaskAdapter) Run(fn func()) bool {
	return p.lifecycle.Run(fn, p.runner)
}
