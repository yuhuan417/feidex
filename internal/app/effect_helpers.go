package app

import (
	"context"

	"feidex/internal/application"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
)

// replyCardEffect routes a user-visible card through the application effect
// runner. Callers keep ownership of rendering while transport details stay at
// the composition boundary.
func replyCardEffect(a *App, msg *feishu.InboundMessage, card map[string]any) error {
	if a == nil || msg == nil {
		return nil
	}
	return newEffectRunner(a).Run(context.Background(), []application.Effect{application.SendCard{
		Frontend:       identity.FrontendID(a.FrontendID()),
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		View:           card,
		InThread:       replyInThreadEnabled(a, msg.ChatType),
	}})
}

func replyTextEffect(a *App, msg *feishu.InboundMessage, text string) error {
	if a == nil || msg == nil {
		return nil
	}
	return newEffectRunner(a).Run(context.Background(), []application.Effect{application.SendMessage{
		Frontend:       identity.FrontendID(a.FrontendID()),
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		Text:           text,
		InThread:       replyInThreadEnabled(a, msg.ChatType),
	}})
}
