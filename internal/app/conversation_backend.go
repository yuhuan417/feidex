package app

import appconvbackend "feidex/internal/app/convbackend"

func conversationBackend(a *App) appconvbackend.ConversationBackendFacade {
	if runtime := backendRuntime(a); runtime != nil {
		return runtime.conversationBackend(a)
	}
	return appconvbackend.NewCodexConversationBackend(a)
}
