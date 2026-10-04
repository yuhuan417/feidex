package feishuapp

import "feidex/internal/domain/identity"

type outboundCardService struct {
	app         *App
	replyChunks replyChunkDelivery
	asyncInput  asyncUserInputCardSender
}

func newOutboundCardService(app *App) outboundCardService {
	if app == nil {
		return outboundCardService{}
	}
	pendingDelivery := pendingCardDeliveryService{
		interactions: app.bindings.InteractionDelivery, lifecycle: &app.runtimeOwner.Lifecycle,
		frontend: identity.FrontendID(app.FrontendID()), deduper: app.runtimeOwner.EffectDeduper,
		turns: app.bindings.TurnPresentation, runner: *app.runtimeOwner.EffectRunner, ready: app.feishu != nil,
	}
	return outboundCardService{app: app, asyncInput: asyncUserInputCardSender{pending: app.State(), delivery: pendingDelivery}, replyChunks: newReplyChunkDelivery(
		newCardRenderer(app.Config()), app.State(), newEffectOutbound(app.FrontendID(), newEffectRunner(app.runtimeOwner)), app.feishu != nil,
	)}
}
