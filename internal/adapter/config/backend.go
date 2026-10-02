package config

import (
	"fmt"
	"path/filepath"
	"sync"

	"feidex/internal/application/backendconfig"
	fileconfig "feidex/internal/config"
)

var _ backendconfig.Repository = (*BackendRepository)(nil)

type BackendSource interface {
	Config() *fileconfig.Config
	ConfigMu() *sync.RWMutex
	ConfigPath() string
	FrontendConfigIndex() int
}

type BackendRepository struct{ source BackendSource }

func NewBackendRepository(source BackendSource) *BackendRepository {
	return &BackendRepository{source: source}
}

func (r *BackendRepository) SetBackend(target string) error {
	if r == nil || r.source == nil || r.source.Config() == nil || r.source.ConfigMu() == nil {
		return fmt.Errorf("backend configuration source is unavailable")
	}
	r.source.ConfigMu().Lock()
	defer r.source.ConfigMu().Unlock()
	next := fileconfig.Clone(r.source.Config())
	idx := r.source.FrontendConfigIndex()
	if idx >= 0 && idx < len(next.Frontends) {
		next.Frontends[idx].Backend = target
	} else {
		next.Feishu.Backend = target
	}
	if err := next.Normalize(filepath.Dir(r.source.ConfigPath())); err != nil {
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
