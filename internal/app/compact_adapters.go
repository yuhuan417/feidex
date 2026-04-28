package app

import (
	"feidex/internal/app/appstate"
	appcompact "feidex/internal/app/compact"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type compactSessionStoreAdapter struct {
	store *appstate.Store
}

func (a compactSessionStoreAdapter) GetSession(key string) *state.Session {
	if a.store == nil {
		return nil
	}
	return a.store.Session(key)
}

func (a compactSessionStoreAdapter) AllSessions() []*state.Session {
	if a.store == nil {
		return nil
	}
	return a.store.Sessions()
}

func (a compactSessionStoreAdapter) SaveSession(sess *state.Session) error {
	if a.store == nil {
		return nil
	}
	return a.store.SaveSession(sess)
}

// ---------------------------------------------------------------------------
// *App methods satisfying compact.App
// ---------------------------------------------------------------------------

func (a *App) SessionStore() appcompact.SessionStore {
	if a == nil {
		return nil
	}
	return compactSessionStoreAdapter{store: a.State()}
}

func (a *App) HandleBackendCompactCommand(msg *feishu.InboundMessage) error {
	return handleBackendCompactCommand(a, msg)
}

func (a *App) RunBackendCompactAction(sessionKey string, svc *appcompact.Service, action any) error {
	actions := backendActions(a)
	if actions == nil {
		return nil
	}
	var cardAction *feishu.CardAction
	if action != nil {
		cardAction, _ = action.(*feishu.CardAction)
	}
	return actions.runMenuCompactActionWithService(a, cardAction, sessionKey, svc)
}
