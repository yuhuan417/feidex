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
func ForwardProcessor(a *App) func(*application.InboundMessage) error {
	return func(msg *application.InboundMessage) error {
		var err error
		runSession(a, makeSessionKey(a, msg), func() { err = a.bindings.Inbound.ProcessMessage(msg) })
		return err
	}
}
func (p forwardPorts) ResolveForward(ctx context.Context, id string, ids []string) (string, []application.Attachment, error) {
	return p.app.feishu.ResolveMergeForward(ctx, id, ids)
}
func (p forwardPorts) Run(fn func()) bool { return runAsync(p.app, fn) }
