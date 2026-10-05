package feishuapp

func newOutboundCardService(app *Frontend) OutboundCardService {
	if app == nil {
		return OutboundCardService{}
	}
	return NewOutboundCardService(OutboundCardInputs{
		RuntimeDeps: app.BackendRuntimeDeps(), Feishu: app.feishu, AsyncRunner: app.asyncRunner,
		InteractionDelivery: app.bindings.InteractionDelivery, TurnPresentation: app.bindings.TurnPresentation,
		Continuation: app.bindings.Continuation, FinalCardPatch: app.bindings.FinalCardPatch,
		TurnFinalFooter: app.bindings.TurnMetadata.TurnFinalFooterLines,
	})
}
