package feishuapp

import (
	"time"

	"feidex/internal/domain/identity"
)

type outboundCardService struct {
	replyChunks          replyChunkDelivery
	statusCards          simpleStatusCardRenderer
	asyncInput           asyncUserInputCardSender
	links                messageLinkRecorder
	localFiles           localFileLinkPatcher
	turnFinalFooterLines func(string, time.Time) []string
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
	view := app.configView()
	links := newMessageLinkRecorder(view, app.runtimeOwner, app.bindings.Continuation)
	localFiles := newLocalFileLinkPatcher(app.Config(), app.State(), app.feishu, &app.runtimeOwner.Lifecycle, app.asyncRunner, app.bindings.FinalCardPatch, outbound, app.feishu != nil)
	return outboundCardService{statusCards: simpleStatusCardRenderer{client: app.feishu}, asyncInput: asyncUserInputCardSender{pending: app.State(), delivery: pendingDelivery}, replyChunks: newReplyChunkDelivery(
		newCardRenderer(app.Config()), app.State(), outbound, app.feishu != nil, view, links, localFiles,
	), links: links, localFiles: localFiles, turnFinalFooterLines: app.bindings.TurnMetadata.TurnFinalFooterLines}
}
