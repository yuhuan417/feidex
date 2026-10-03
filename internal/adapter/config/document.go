package config

import (
	"fmt"
	"path/filepath"

	fileconfig "feidex/internal/config"
)

func configDir(path string) string { return filepath.Dir(path) }

// DocumentRepository owns transactional updates to the config document. It
// keeps normalization, persistence and publication out of command services.
type DocumentRepository struct{ source Source }

func NewDocumentRepository(source Source) *DocumentRepository {
	return &DocumentRepository{source: source}
}

func (r *DocumentRepository) UpdateConfig(mutate func(*fileconfig.Config) error) error {
	if r == nil || r.source == nil || r.source.Config() == nil || r.source.ConfigMu() == nil {
		return fmt.Errorf("configuration source is unavailable")
	}
	if mutate == nil {
		return fmt.Errorf("configuration mutation is required")
	}
	r.source.ConfigMu().Lock()
	defer r.source.ConfigMu().Unlock()
	next := fileconfig.Clone(r.source.Config())
	if err := mutate(next); err != nil {
		return err
	}
	if err := next.Normalize(configDir(r.source.ConfigPath())); err != nil {
		return err
	}
	if r.source.ConfigPath() != "" {
		if err := fileconfig.Save(r.source.ConfigPath(), next); err != nil {
			return err
		}
	}
	*r.source.Config() = *next
	return nil
}
