package outbound

import (
	"feidex/internal/application/presentation"
	"feidex/internal/feishu"
	"fmt"
)

// RenderedCard belongs to the Feishu adapter. Existing complex card renderers
// can pass their platform payload without leaking an untyped view into effects.
type RenderedCard struct{ Payload map[string]any }

func (RenderedCard) CardView()                          {}
func Card(payload map[string]any) presentation.CardView { return RenderedCard{Payload: payload} }
func Render(view presentation.CardView) (map[string]any, error) {
	switch card := view.(type) {
	case RenderedCard:
		return card.Payload, nil
	case presentation.StatusCard:
		buttons := make([]feishu.Button, 0, len(card.Actions))
		for _, action := range card.Actions {
			buttons = append(buttons, feishu.Button{Text: action.Text, Type: action.Style, Value: action.Value})
		}
		return feishu.SimpleStatusCard(card.Title, card.Color, card.Body, buttons), nil
	default:
		return nil, fmt.Errorf("invalid card view %T", view)
	}
}
