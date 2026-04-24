package app

import (
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// dispatchCardAction is the synchronous Feishu callback entrypoint.
// Handlers must acknowledge quickly and must not block on heavy workflows.
func (a *App) dispatchCardAction(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return newCardActionService(a).dispatch(action)
}
