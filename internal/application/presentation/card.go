package presentation

// CardView describes a presentation value. Platform payloads implement this
// at the adapter boundary; use cases use the semantic StatusCard below.
type CardView interface{ CardView() }
type StatusCard struct {
	Title, Color, Body string
	Actions            []Action
}

func (StatusCard) CardView() {}

type Action struct {
	Text, Style string
	Value       map[string]any
}
