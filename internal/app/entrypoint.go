package app

import (
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// HandlerSet is the narrow Feishu transport surface required by the entry
// boundary. It deliberately excludes outbound business operations.
type HandlerSet interface {
	SetHandlers(func(*feishu.InboundMessage), func(*feishu.CardAction) (*callback.CardActionTriggerResponse, error), func(*feishu.MessageRecall), func(*feishu.MessageReaction))
	ConfigureLocalFileLinks(string, string)
}

// Entrypoint is the narrow event surface implemented by the composed
// Feishu frontend. The implementation owns application/runtime behavior; this
// package only binds Feishu facts to it.
type Entrypoint interface {
	HandleFeishuMessage(*feishu.InboundMessage)
	HandleCardAction(*feishu.CardAction) (*callback.CardActionTriggerResponse, error)
	HandleFeishuRecall(*feishu.MessageRecall)
	HandleFeishuReaction(*feishu.MessageReaction)
}

// BindHandlers installs the Feishu event callbacks after composition has
// created the frontend. No application state or service is constructed here.
func BindHandlers(transport HandlerSet, entry Entrypoint) {
	if transport == nil || entry == nil {
		return
	}
	transport.SetHandlers(entry.HandleFeishuMessage, entry.HandleCardAction, entry.HandleFeishuRecall, entry.HandleFeishuReaction)
	transport.ConfigureLocalFileLinks("", "")
}
