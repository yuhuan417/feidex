package feishuapp

import (
	"time"

	"feidex/internal/adapter/feishu/finalcardpatch"
	appturnstream "feidex/internal/adapter/feishu/turnstream"
	"feidex/internal/application/continuation"
	applicationinteraction "feidex/internal/application/interaction"
	"feidex/internal/domain/identity"
)

type OutboundCardService struct {
	replyChunks          replyChunkDelivery
	statusCards          simpleStatusCardRenderer
	asyncInput           asyncUserInputCardSender
	links                messageLinkRecorder
	localFiles           localFileLinkPatcher
	turnFinalFooterLines func(string, time.Time) []string
}

type OutboundCardInputs struct {
	RuntimeDeps         BackendRuntimeDeps
	Feishu              FeishuClient
	AsyncRunner         func(func())
	InteractionDelivery *applicationinteraction.DeliveryService
	TurnPresentation    *appturnstream.Service
	Continuation        *continuation.Service
	FinalCardPatch      finalcardpatch.Service
	TurnFinalFooter     func(string, time.Time) []string
}

func NewOutboundCardService(inputs OutboundCardInputs) OutboundCardService {
	deps := inputs.RuntimeDeps
	owner := deps.runtime.owner
	if owner == nil {
		return OutboundCardService{}
	}
	pendingDelivery := NewPendingCardDeliveryService(PendingCardDeliveryInputs{
		Interactions: inputs.InteractionDelivery, Lifecycle: &owner.Lifecycle,
		Frontend: identity.FrontendID(deps.frontendID), Deduper: owner.EffectDeduper,
		Turns: inputs.TurnPresentation, Runner: *owner.EffectRunner, Ready: inputs.Feishu != nil,
	})
	state := deps.stateView
	outbound := newEffectOutbound(deps.frontendID, *owner.EffectRunner)
	view := deps.view
	links := newMessageLinkRecorder(view, owner, inputs.Continuation)
	localFiles := newLocalFileLinkPatcher(deps.cfg, state, inputs.Feishu, &owner.Lifecycle, inputs.AsyncRunner, inputs.FinalCardPatch, outbound, inputs.Feishu != nil)
	return OutboundCardService{statusCards: simpleStatusCardRenderer{client: inputs.Feishu}, asyncInput: asyncUserInputCardSender{pending: state, delivery: pendingDelivery}, replyChunks: newReplyChunkDelivery(
		newCardRenderer(deps.cfg), state, outbound, inputs.Feishu != nil, view, links, localFiles,
	), links: links, localFiles: localFiles, turnFinalFooterLines: inputs.TurnFinalFooter}
}
