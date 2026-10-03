package modelconfig

import (
	"fmt"
	"strings"
)

type OptionsRepository interface {
	UpdateModelOptions(func([]string) ([]string, error)) error
}
type OptionsService struct{ Repository OptionsRepository }

func (s OptionsService) Set(value string, add bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("model id must not be empty")
	}
	return s.Repository.UpdateModelOptions(func(current []string) ([]string, error) {
		if add {
			current = append(current, value)
		}
		out := make([]string, 0, len(current))
		seen := map[string]bool{}
		for _, model := range current {
			model = strings.TrimSpace(model)
			if model == "" || seen[model] || (!add && model == value) {
				continue
			}
			out = append(out, model)
			seen[model] = true
		}
		return out, nil
	})
}
