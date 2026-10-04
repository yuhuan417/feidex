package feishuapp

type outboundCardService struct {
	app         *App
	replyChunks replyChunkDelivery
}

func newOutboundCardService(app *App) outboundCardService {
	if app == nil {
		return outboundCardService{}
	}
	return outboundCardService{app: app, replyChunks: newReplyChunkDelivery(
		newCardRenderer(app.Config()), app.State(), newEffectOutbound(app.FrontendID(), newEffectRunner(app.runtimeOwner)), app.feishu != nil,
	)}
}
