// Package runtimeconfig owns frontend-scoped runtime preference updates.
package runtimeconfig

import "fmt"

type Repository interface {
	SetQuietMode(string) error
	SetAutoRetry(bool) error
	SetLogLevel(string) error
}

type Service struct{ Repository Repository }

func (s Service) SetQuietMode(mode string) error {
	if s.Repository == nil {
		return fmt.Errorf("runtime configuration repository is nil")
	}
	if mode == "" {
		return fmt.Errorf("quiet mode is required")
	}
	return s.Repository.SetQuietMode(mode)
}

func (s Service) SetAutoRetry(enabled bool) error {
	if s.Repository == nil {
		return fmt.Errorf("runtime configuration repository is nil")
	}
	return s.Repository.SetAutoRetry(enabled)
}

func (s Service) SetLogLevel(level string) error {
	if s.Repository == nil {
		return fmt.Errorf("runtime configuration repository is nil")
	}
	if level == "" {
		return fmt.Errorf("log level is required")
	}
	return s.Repository.SetLogLevel(level)
}
