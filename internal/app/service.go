package app

import (
	"context"
	"fmt"
	"path/filepath"

	"feidex/internal/config"
	"feidex/internal/state"
)

type Service struct {
	cfg   *config.Config
	store *state.Store
	apps  []*App
}

func NewService(cfg *config.Config, cfgPath string) (*Service, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	store, err := state.Open(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	frontends := cfg.ResolvedFrontends()
	if len(frontends) == 0 {
		return nil, fmt.Errorf("no frontend configured")
	}
	apps := make([]*App, 0, len(frontends))
	for _, frontend := range frontends {
		app, err := newFrontendApp(cfg, cfgPath, store, frontend)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	return &Service{
		cfg:   cfg,
		store: store,
		apps:  apps,
	}, nil
}

func (s *Service) Start(ctx context.Context) error {
	started := make([]*App, 0, len(s.apps))
	for _, app := range s.apps {
		if err := startBackend(app, ctx); err != nil {
			_ = stopApps(ctx, started)
			return err
		}
		started = append(started, app)
	}
	for _, app := range s.apps {
		startInboundDeduperLoop(app, ctx)
	}
	if len(s.apps) > 0 {
		recoverSharedRuntimeState(s.apps[0])
	}
	for _, app := range s.apps {
		recoverFrontendRuntimeState(app)
	}
	for _, app := range s.apps {
		if err := startFrontend(app, ctx); err != nil {
			_ = stopApps(ctx, s.apps)
			return err
		}
	}
	for _, app := range s.apps {
		newRuntimeMaintenanceService(app).StartDriveArtifactGCLoop(ctx)
		newRuntimeMaintenanceService(app).StartUpgradeCheckLoop(ctx)
		go sendStartupReadyNotifications(app)
	}
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	return stopApps(ctx, s.apps)
}

func stopApps(ctx context.Context, apps []*App) error {
	var firstErr error
	for i := len(apps) - 1; i >= 0; i-- {
		if err := apps[i].Stop(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
