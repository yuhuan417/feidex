package application

import "testing"

func TestValuesPreserveTypedCardCallbackValues(t *testing.T) {
	values := ValuesFromMap(map[string]any{
		"action":  "menu.model",
		"page":    2,
		"checked": true,
		"options": []string{"a", "b"},
	})
	if got, ok := values.String("action"); !ok || got != "menu.model" {
		t.Fatalf("action = %q, %v", got, ok)
	}
	if got, ok := values.Int("page"); !ok || got != 2 {
		t.Fatalf("page = %d, %v", got, ok)
	}
	if got, ok := values.Bool("checked"); !ok || !got {
		t.Fatalf("checked = %v, %v", got, ok)
	}
	if got, ok := values.Strings("options"); !ok || len(got) != 2 || got[1] != "b" {
		t.Fatalf("options = %#v, %v", got, ok)
	}
	roundTrip := values.Map()
	if got, ok := roundTrip["page"].(float64); !ok || got != 2 {
		t.Fatalf("round-trip page = %#v", roundTrip["page"])
	}
}
