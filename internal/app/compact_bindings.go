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
	svc := appcompact.NewService(a)
	return newBackendActionService(a).HandleCompactCommand(msg, &svc)
}

func (a *App) RunBackendCompactAction(sessionKey string, svc *appcompact.Service, action any) error {
	var cardAction *feishu.CardAction
	if action != nil {
		cardAction, _ = action.(*feishu.CardAction)
	}
	return newBackendActionService(a).RunMenuCompactAction(cardAction, sessionKey, svc)
}
