package codexruntime

import (
	"context"
	"errors"
	"fmt"
	"os"

	"feidex/internal/codexrpc"
)

// UpgradeService manages Codex runtime upgrade and restart operations.
// All host-app dependencies are injected as callback function fields.
type UpgradeService struct {
	// CreateClient creates a new Codex client with the given config.
	CreateClient func() CodexClient

	// ConfigureClient configures a Codex client with handlers and
	// error handling. Called after CreateClient.
	ConfigureClient func(client CodexClient)

	// ClientExperimentalAPI returns whether the experimental API is enabled.
	ClientExperimentalAPI func() bool

	// IsBackendActive reports whether the codex backend is active.
	IsBackendActive func() bool

	// SmokeTest runs a smoke test on the codex backend.
	SmokeTest func(ctx context.Context) error

	// CurrentClient returns the current Codex client.
	CurrentClient func() CodexClient

	// ReplaceClient replaces the current Codex client.
	ReplaceClient func(next CodexClient) CodexClient

	// RecoverFrontendRuntimeState rebuilds frontend-scoped thread bindings after
	// the runtime client has been replaced.
	RecoverFrontendRuntimeState func()
}

// NewUpgradeService creates a new UpgradeService.
func NewUpgradeService() UpgradeService {
	return UpgradeService{}
}

// StartVerifiedCodexClient creates, configures, starts, and verifies a
// new Codex client. The caller is responsible for closing the returned
// client if it is no longer needed.
func (s UpgradeService) StartVerifiedCodexClient(ctx context.Context) (CodexClient, error) {
	if s.CreateClient == nil {
		return nil, fmt.Errorf("codex client factory not configured")
	}
	client := s.CreateClient()
	if client == nil {
		return nil, fmt.Errorf("codex client not initialized")
	}
	if s.ConfigureClient != nil {
		s.ConfigureClient(client)
	}
	experimentalAPI := false
	if s.ClientExperimentalAPI != nil {
		experimentalAPI = s.ClientExperimentalAPI()
	}
	if err := client.Start(ctx, experimentalAPI); err != nil {
		return nil, err
	}
	var result codexrpc.ModelListResult
	if err := client.Call(ctx, "model/list", map[string]any{"limit": 1, "includeHidden": false}, &result); err != nil {
		_ = client.Close()
		return nil, err
	}
	if len(result.Data) == 0 {
		_ = client.Close()
		return nil, fmt.Errorf("model/list returned no visible models")
	}
	return client, nil
}

// CodexSmokeTest runs a smoke test by starting and closing a verified client.
func (s UpgradeService) CodexSmokeTest(ctx context.Context) error {
	client, err := s.StartVerifiedCodexClient(ctx)
	if err != nil {
		return err
	}
	return client.Close()
}

// RefreshRuntimeAfterMaintenance refreshes the Codex runtime after a
// maintenance operation (upgrade or restart). Returns (true, nil) if the
// runtime was switched, (false, nil) if only a smoke test was performed.
func (s UpgradeService) RefreshRuntimeAfterMaintenance(ctx context.Context) (bool, error) {
	if s.IsBackendActive == nil {
		return false, fmt.Errorf("app not initialized")
	}
	if !s.IsBackendActive() {
		if s.SmokeTest != nil {
			return false, s.SmokeTest(ctx)
		}
		return false, s.CodexSmokeTest(ctx)
	}
	next, err := s.StartVerifiedCodexClient(ctx)
	if err != nil {
		return false, err
	}
	if s.CurrentClient != nil && s.ReplaceClient != nil {
		old := s.CurrentClient()
		if old != nil {
			if err := old.Close(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				_ = next.Close()
				return false, fmt.Errorf("切换 runtime 失败: %w", err)
			}
		}
		s.ReplaceClient(next)
		if s.RecoverFrontendRuntimeState != nil {
			s.RecoverFrontendRuntimeState()
		}
		return true, nil
	}
	_ = next.Close()
	return false, fmt.Errorf("runtime replacement not configured")
}
