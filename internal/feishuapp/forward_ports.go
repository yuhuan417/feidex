package feishuapp

import (
	"context"
	"feidex/internal/application"
	"feidex/internal/application/inbound"
)

type forwardPorts struct{ app *App }

func ForwardGateway(a *App) inbound.ForwardGateway { return forwardPorts{app: a} }
func ForwardTasks(a *App) inbound.ForwardTasks     { return forwardPorts{app: a} }
func ForwardFailure(a *App) func(*application.InboundMessage, error) {
	return func(msg *application.InboundMessage, err error) { _ = replyError(a, msg, err) }
}

// ForwardProcessor takes the inbound entry point as a value rather than
// reaching for a.bindings.Inbound, so composition can wire the two services
// together after both exist instead of them holding each other.
func ForwardProcessor(a *App, process func(*application.InboundMessage) error) func(*application.InboundMessage) error {
	return func(msg *application.InboundMessage) error {
		var err error
		runSession(a, a.configView().makeSessionKey(msg), func() { err = process(msg) })
		return err
	}
}
func (p forwardPorts) ResolveForward(ctx context.Context, id string, ids []string) (string, []application.Attachment, error) {
	return p.app.feishu.ResolveMergeForward(ctx, id, ids)
}
func (p forwardPorts) Run(fn func()) bool { return runAsync(p.app, fn) }
