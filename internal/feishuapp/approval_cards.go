package feishuapp

import "feidex/internal/domain/identity"

type outboundCardService struct {
	app         *App
	replyChunks replyChunkDelivery
	asyncInput  asyncUserInputCardSender
	links       messageLinkRecorder
	localFiles  localFileLinkPatcher
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
	outbound := newEffectOutbound(app.FrontendID(), newEffectRunner(app.runtimeOwner))
	return outboundCardService{app: app, asyncInput: asyncUserInputCardSender{pending: app.State(), delivery: pendingDelivery}, replyChunks: newReplyChunkDelivery(
		newCardRenderer(app.Config()), app.State(), outbound, app.feishu != nil,
	), links: newMessageLinkRecorder(app.configView(), app.runtimeOwner, app.bindings.Continuation),
		localFiles: newLocalFileLinkPatcher(app.Config(), app.State(), app.feishu, &app.runtimeOwner.Lifecycle, app.asyncRunner, app.bindings.FinalCardPatch, outbound, app.feishu != nil)}
}
