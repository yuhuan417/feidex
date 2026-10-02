// Package config adapts the in-memory config document to the workspace
// configuration application port.
package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	appworkspace "feidex/internal/application/workspace"
	fileconfig "feidex/internal/config"
	domain "feidex/internal/domain/workspace"
)

var _ appworkspace.ConfigurationRepository = (*WorkspaceRepository)(nil)

type Source interface {
	Config() *fileconfig.Config
	ConfigMu() *sync.RWMutex
	ConfigPath() string
}

type WorkspaceRepository struct{ source Source }

func NewWorkspaceRepository(source Source) *WorkspaceRepository {
	return &WorkspaceRepository{source: source}
}

func (r *WorkspaceRepository) List() []domain.Workspace {
	if r == nil || r.source == nil || r.source.Config() == nil || r.source.ConfigMu() == nil {
		return nil
	}
	r.source.ConfigMu().RLock()
	defer r.source.ConfigMu().RUnlock()
	return append([]domain.Workspace(nil), r.source.Config().Workspaces...)
}

func (r *WorkspaceRepository) Get(id string) (*domain.Workspace, error) {
	if r == nil || r.source == nil || r.source.Config() == nil || r.source.ConfigMu() == nil {
		return nil, fmt.Errorf("workspace configuration source is unavailable")
	}
	id = strings.TrimSpace(id)
	r.source.ConfigMu().RLock()
	defer r.source.ConfigMu().RUnlock()
	for i := range r.source.Config().Workspaces {
		if strings.TrimSpace(r.source.Config().Workspaces[i].ID) == id {
			value := r.source.Config().Workspaces[i]
			return &value, nil
		}
	}
	return nil, nil
}

func (r *WorkspaceRepository) Create(input domain.Workspace) (*domain.Workspace, error) {
	return r.mutate(func(cfg *fileconfig.Config) (*domain.Workspace, error) {
		if fileconfig.FindWorkspace(cfg, input.ID) != nil {
			return nil, fmt.Errorf("workspace %q 已存在", input.ID)
		}
		cfg.Workspaces = append(cfg.Workspaces, input)
		return &cfg.Workspaces[len(cfg.Workspaces)-1], nil
	})
}

func (r *WorkspaceRepository) Update(id string, mutate func(*domain.Workspace)) (*domain.Workspace, error) {
	return r.mutate(func(cfg *fileconfig.Config) (*domain.Workspace, error) {
		ws := fileconfig.FindWorkspace(cfg, id)
		if ws == nil {
			return nil, fmt.Errorf("workspace %q not found", id)
		}
		mutate(ws)
		return ws, nil
	})
}

func (r *WorkspaceRepository) Delete(id string) (string, error) {
	if r == nil || r.source == nil || r.source.Config() == nil || r.source.ConfigMu() == nil {
		return "", fmt.Errorf("workspace configuration source is unavailable")
	}
	id = strings.TrimSpace(id)
	r.source.ConfigMu().Lock()
	defer r.source.ConfigMu().Unlock()
	candidate := fileconfig.Clone(r.source.Config())
	if fileconfig.FindWorkspace(candidate, id) == nil {
		return "", fmt.Errorf("workspace %q not found", id)
	}
	next := make([]fileconfig.Workspace, 0, len(candidate.Workspaces)-1)
	fallback := ""
	for _, ws := range candidate.Workspaces {
		if strings.TrimSpace(ws.ID) == id {
			continue
		}
		if fallback == "" {
			fallback = ws.ID
		}
		next = append(next, ws)
	}
	if fallback == "" {
		return "", fmt.Errorf("至少保留一个 workspace")
	}
	candidate.Workspaces = next
	if err := r.normalizeAndSaveLocked(candidate); err != nil {
		return "", err
	}
	*r.source.Config() = *candidate
	return fallback, nil
}

func (r *WorkspaceRepository) mutate(fn func(*fileconfig.Config) (*domain.Workspace, error)) (*domain.Workspace, error) {
	if r == nil || r.source == nil || r.source.Config() == nil || r.source.ConfigMu() == nil {
		return nil, fmt.Errorf("workspace configuration source is unavailable")
	}
	r.source.ConfigMu().Lock()
	defer r.source.ConfigMu().Unlock()
	candidate := fileconfig.Clone(r.source.Config())
	result, err := fn(candidate)
	if err != nil {
		return nil, err
	}
	if err := r.normalizeAndSaveLocked(candidate); err != nil {
		return nil, err
	}
	*r.source.Config() = *candidate
	if result == nil {
		return nil, nil
	}
	copy := *result
	return &copy, nil
}

func (r *WorkspaceRepository) normalizeAndSaveLocked(cfg *fileconfig.Config) error {
	if err := cfg.Normalize(filepath.Dir(r.source.ConfigPath())); err != nil {
		return err
	}
	return fileconfig.Save(r.source.ConfigPath(), cfg)
}
