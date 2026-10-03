package config

import fileconfig "feidex/internal/config"

type ModelOptionsRepository struct{ Source Source }

func (r ModelOptionsRepository) UpdateModelOptions(mutate func([]string) ([]string, error)) error {
	return NewDocumentRepository(r.Source).UpdateConfig(func(cfg *fileconfig.Config) error {
		options, err := mutate(append([]string(nil), cfg.Claude.ModelOptions...))
		if err == nil {
			cfg.Claude.ModelOptions = options
		}
		return err
	})
}
