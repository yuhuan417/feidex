package workspace

import (
	"fmt"
	"strings"

	domain "feidex/internal/domain/workspace"
)

// ConfigurationRepository is the persistence port for workspace definitions.
// Normalization and file format details stay in the storage adapter.
type ConfigurationRepository interface {
	List() []domain.Workspace
	Get(string) (*domain.Workspace, error)
	Create(domain.Workspace) (*domain.Workspace, error)
	Update(string, func(*domain.Workspace)) (*domain.Workspace, error)
	Delete(string) (string, error)
}

// ConfigurationService owns workspace configuration mutations. Session
// switching and thread binding remain separate application capabilities.
type ConfigurationService struct {
	Repository ConfigurationRepository
}

func (s ConfigurationService) Create(input domain.Workspace) (*domain.Workspace, error) {
	if s.Repository == nil {
		return nil, fmt.Errorf("workspace configuration repository is nil")
	}
	input.ID = strings.TrimSpace(input.ID)
	if input.ID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	return s.Repository.Create(input)
}

func (s ConfigurationService) Update(id string, mutate func(*domain.Workspace)) (*domain.Workspace, error) {
	if s.Repository == nil {
		return nil, fmt.Errorf("workspace configuration repository is nil")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	if mutate == nil {
		return nil, fmt.Errorf("workspace mutation is required")
	}
	return s.Repository.Update(id, mutate)
}

func (s ConfigurationService) Delete(id string) (string, error) {
	if s.Repository == nil {
		return "", fmt.Errorf("workspace configuration repository is nil")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("workspace id is required")
	}
	return s.Repository.Delete(id)
}
