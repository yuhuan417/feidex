package pathpicker

import (
	pickerapp "feidex/internal/application/pathpicker"
	domain "feidex/internal/domain/workspace"
)

type Views interface {
	Snapshot(domain.PathPickerPayload) (pickerapp.View, error)
}
type Presentation struct{ Views Views }

func (p Presentation) RenderCard(id string, payload domain.PathPickerPayload) (map[string]any, error) {
	view, err := p.Views.Snapshot(payload)
	if err != nil {
		return nil, err
	}
	return RenderCard(id, view), nil
}
