package app

import (
	"feidex/internal/composition"
	"feidex/internal/config"
)

func New(cfg *config.Config, path string) (*App, error) {
	return composition.New(cfg, path, NewFrontend)
}
