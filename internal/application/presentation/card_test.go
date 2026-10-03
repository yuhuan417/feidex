package presentation

import (
	"encoding/json"
	"testing"
)

func TestStatusCardActionUsesOpaqueJSON(t *testing.T) {
	value, err := json.Marshal(map[string]string{"action": "menu.root"})
	if err != nil {
		t.Fatal(err)
	}
	card := StatusCard{Actions: []Action{{Text: "back", Style: "default", Value: value}}}
	if len(card.Actions[0].Value) == 0 {
		t.Fatal("action value was not retained")
	}
}
