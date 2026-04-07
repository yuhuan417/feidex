package app

import "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}
