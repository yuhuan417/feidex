package app

type conversationWorkflowService struct {
	app *App
}

func newConversationWorkflowService(app *App) conversationWorkflowService {
	return conversationWorkflowService{app: app}
}
