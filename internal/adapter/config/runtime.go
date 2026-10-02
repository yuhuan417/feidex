package config

import (
	"fmt"
	"path/filepath"

	"feidex/internal/application/runtimeconfig"
	fileconfig "feidex/internal/config"
)

var _ runtimeconfig.Repository = (*RuntimeRepository)(nil)

type RuntimeRepository struct{ source BackendSource }

func NewRuntimeRepository(source BackendSource) *RuntimeRepository {
	return &RuntimeRepository{source: source}
}

func (r *RuntimeRepository) SetQuietMode(mode string) error {
	parsed, err := fileconfig.ParseQuietMode(fileconfig.QuietMode(mode))
	if err != nil {
		return err
	}
	return r.mutate(func(cfg *fileconfig.FeishuConfig) { cfg.Quiet = parsed })
}

func (r *RuntimeRepository) SetAutoRetry(enabled bool) error {
	return r.mutate(func(cfg *fileconfig.FeishuConfig) { cfg.AutoRetry = enabled })
}

func (r *RuntimeRepository) SetLogLevel(level string) error {
	if r == nil || r.source == nil || r.source.Config() == nil || r.source.ConfigMu() == nil {
		return fmt.Errorf("runtime configuration source is unavailable")
	}
	r.source.ConfigMu().Lock()
	defer r.source.ConfigMu().Unlock()
	next := fileconfig.Clone(r.source.Config())
	next.Log.Level = level
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

func (r *RuntimeRepository) mutate(update func(*fileconfig.FeishuConfig)) error {
	if r == nil || r.source == nil || r.source.Config() == nil || r.source.ConfigMu() == nil {
		return fmt.Errorf("runtime configuration source is unavailable")
	}
	r.source.ConfigMu().Lock()
	defer r.source.ConfigMu().Unlock()
	next := fileconfig.Clone(r.source.Config())
	idx := r.source.FrontendConfigIndex()
	if idx >= 0 && idx < len(next.Frontends) {
		update(&next.Frontends[idx].FeishuConfig)
	} else {
		update(&next.Feishu)
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
