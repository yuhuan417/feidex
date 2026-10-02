// Package backendconfig owns persistence of the selected backend for a
// frontend. Runtime switching remains a separate lifecycle use case.
package backendconfig

import (
	"fmt"
	"strings"

	domain "feidex/internal/domain/backend"
)

type Repository interface {
	SetBackend(string) error
}

type Service struct{ Repository Repository }

func (s Service) SetBackend(target string) error {
	target = domain.NormalizeBackend(target)
	if target == "" {
		return fmt.Errorf("missing backend")
	}
	if s.Repository == nil {
		return fmt.Errorf("backend configuration repository is nil")
	}
	return s.Repository.SetBackend(strings.TrimSpace(target))
}
