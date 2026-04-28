package app

import (
	"encoding/json"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
