package app

import (
	"feidex/internal/app/appstate"
	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Indirect function references to break initialization cycles.
// Set in init() so adapter constructors don't statically reference
// functions that participate in the menu rendering cycle.
// ---------------------------------------------------------------------------

var indirectCompleteMenuCommand func(a *App, action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error)
var indirectReplyCommandActionResponse func(a *App, msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error

func init() {
	indirectCompleteMenuCommand = func(a *App, action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
		return completeMenuCommand(a, action, sessionKey, rawCommand, parentAction)
	}
	indirectReplyCommandActionResponse = func(a *App, msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
		return replyCommandActionResponse(a, msg, resp)
	}
}

func workspaceStateDeps(store *appstate.Store) appworkspacecmd.StateDeps {
	return appworkspacecmd.StateDeps{
		GetSession:    func(key string) *state.Session { return store.Session(key) },
		Sessions:      func() []*state.Session { return store.Sessions() },
		SaveSession:   func(sess *state.Session) error { return store.SaveSession(sess) },
		NextLocalID:   func(prefix string) (string, error) { return store.NextLocalID(prefix) },
		Pending:       func(id string) *state.PendingRequest { return store.Pending(id) },
		SavePending:   func(req *state.PendingRequest) error { return store.SavePending(req) },
		UpdatePending: func(id string, mutate func(*state.PendingRequest)) error { return store.UpdatePending(id, mutate) },
	}
}
