// Package codexruntime provides Codex-specific runtime recovery and upgrade
// operations extracted from the app god package. Sub-packages cannot import
// app/, so all host-app dependencies are injected as callback function fields.
package codexruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	appcore "feidex/internal/app/appcore"
)

// CodexClient is the interface for the Codex RPC client, re-exported
// from appcore for use by this package.
type CodexClient = appcore.CodexClient

// RecoveryState holds synchronized state for Codex transport recovery.
type RecoveryState struct {
	mu             sync.Mutex
	recovering     bool
	recoverySource CodexClient
	client         CodexClient

	autoThreadMu  sync.Mutex
	autoThreading bool
}

// NewRecoveryState creates a new RecoveryState.
func NewRecoveryState() *RecoveryState {
	return &RecoveryState{}
}

// SetRecoveringForTest sets the recovering flag for testing purposes.
func (s *RecoveryState) SetRecoveringForTest() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recovering = true
}

// RecoveryService manages Codex transport recovery. All host-app
// dependencies are injected as callback function fields.
type RecoveryService struct {
	State *RecoveryState

	// FrontendID returns the frontend identifier for logging.
	FrontendID func() string

	// IsBackendActive reports whether the codex backend is active.
	IsBackendActive func() bool

	// StartVerifiedCodexClient starts and verifies a new Codex client.
	StartVerifiedCodexClient func(ctx context.Context) (CodexClient, error)

	// RecoverFrontendRuntimeState recovers frontend runtime state.
	RecoverFrontendRuntimeState func()

	// SessionKeysForRecovery returns session keys that may need resuming.
	SessionKeysForRecovery func() []string

	// SessionShouldStartNextSubmissionAsync reports whether a session
	// should start the next submission.
	SessionShouldStartNextSubmissionAsync func(sessionKey string) bool

	// StartNextSubmissionAsync starts the next submission for a session.
	StartNextSubmissionAsync func(sessionKey, reason string)
}

// NewRecoveryService creates a new RecoveryService.
func NewRecoveryService(state *RecoveryState) RecoveryService {
	return RecoveryService{State: state}
}

// ---------------------------------------------------------------------------
// State accessors
// ---------------------------------------------------------------------------

// CurrentClient returns the current Codex client (thread-safe).
func (s RecoveryService) CurrentClient() CodexClient {
	if s.State == nil {
		return nil
	}
	s.State.mu.Lock()
	defer s.State.mu.Unlock()
	return s.State.client
}

// RequireClient returns the current Codex client or an error.
func (s RecoveryService) RequireClient() (CodexClient, error) {
	client := s.CurrentClient()
	if client == nil {
		return nil, fmt.Errorf("codex client not initialized")
	}
	return client, nil
}

// ReplaceClient replaces the current Codex client (thread-safe).
func (s RecoveryService) ReplaceClient(next CodexClient) CodexClient {
	if s.State == nil {
		return nil
	}
	s.State.mu.Lock()
	defer s.State.mu.Unlock()
	prev := s.State.client
	s.State.client = next
	return prev
}

// ReplyError sends an error reply via the current Codex client.
func (s RecoveryService) ReplyError(requestID json.RawMessage, code int, message string) {
	client := s.CurrentClient()
	if client == nil {
		return
	}
	_ = client.ReplyError(requestID, code, message)
}

// IsRecovering reports whether recovery is in progress.
func (s RecoveryService) IsRecovering() bool {
	if s.State == nil {
		return false
	}
	s.State.mu.Lock()
	defer s.State.mu.Unlock()
	return s.State.recovering
}

// AutoThreadRecoveryActive reports whether auto-thread recovery is active.
func (s RecoveryService) AutoThreadRecoveryActive() bool {
	if s.State == nil {
		return false
	}
	s.State.autoThreadMu.Lock()
	defer s.State.autoThreadMu.Unlock()
	return s.State.autoThreading
}

// BeginAutoThreadRecoveryScope starts an auto-thread recovery scope.
// Returns a cleanup function.
func (s RecoveryService) BeginAutoThreadRecoveryScope() func() {
	if s.State == nil {
		return func() {}
	}
	s.State.autoThreadMu.Lock()
	s.State.autoThreading = true
	s.State.autoThreadMu.Unlock()
	return func() {
		s.State.autoThreadMu.Lock()
		s.State.autoThreading = false
		s.State.autoThreadMu.Unlock()
	}
}

// ---------------------------------------------------------------------------
// Recovery lifecycle
// ---------------------------------------------------------------------------

// BeginRecovery marks the start of transport recovery.
// Returns false if recovery cannot begin.
func (s RecoveryService) BeginRecovery(client CodexClient) bool {
	if s.State == nil || client == nil {
		return false
	}
	s.State.mu.Lock()
	defer s.State.mu.Unlock()
	if s.IsBackendActive == nil || !s.IsBackendActive() {
		return false
	}
	if s.State.recovering || s.State.client != client {
		return false
	}
	s.State.recovering = true
	s.State.recoverySource = client
	s.State.client = nil
	return true
}

// CompleteRecovery marks the end of transport recovery, installing the
// new client. Returns false if recovery state is inconsistent.
func (s RecoveryService) CompleteRecovery(next CodexClient) bool {
	if s.State == nil {
		return false
	}
	s.State.mu.Lock()
	defer s.State.mu.Unlock()
	source := s.State.recoverySource
	s.State.recovering = false
	s.State.recoverySource = nil
	if s.IsBackendActive == nil || !s.IsBackendActive() {
		return false
	}
	if s.State.client != nil && s.State.client != source {
		return false
	}
	s.State.client = next
	return next != nil
}

// RecoverAfterTransportFailure orchestrates the full recovery flow:
// closing the failed client, starting a new verified client, and
// recovering frontend state.
func (s RecoveryService) RecoverAfterTransportFailure(failed CodexClient, skipFrontendRecovery bool) {
	if s.State == nil {
		return
	}
	if failed != nil {
		defer func() {
			if err := failed.Close(); err != nil {
				frontendID := ""
				if s.FrontendID != nil {
					frontendID = s.FrontendID()
				}
				slog.Debug("codex transport failure close skipped",
					"frontend_id", frontendID,
					"error", err,
				)
			}
		}()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if s.StartVerifiedCodexClient == nil {
		_ = s.CompleteRecovery(nil)
		return
	}
	next, err := s.StartVerifiedCodexClient(ctx)
	if err != nil {
		_ = s.CompleteRecovery(nil)
		frontendID := ""
		if s.FrontendID != nil {
			frontendID = s.FrontendID()
		}
		slog.Error("codex runtime recovery failed",
			"frontend_id", frontendID,
			"error", err,
		)
		return
	}
	if !s.CompleteRecovery(next) {
		_ = next.Close()
		return
	}
	frontendID := ""
	if s.FrontendID != nil {
		frontendID = s.FrontendID()
	}
	slog.Info("codex runtime recovered",
		"frontend_id", frontendID,
		"frontend_thread_recovery_skipped", skipFrontendRecovery,
	)
	if !skipFrontendRecovery && s.RecoverFrontendRuntimeState != nil {
		s.RecoverFrontendRuntimeState()
	}
	s.ResumeQueuedSessions()
}

// ResumeQueuedSessions resumes queued sessions after Codex runtime recovery.
func (s RecoveryService) ResumeQueuedSessions() {
	if s.SessionKeysForRecovery == nil || s.SessionShouldStartNextSubmissionAsync == nil || s.StartNextSubmissionAsync == nil {
		return
	}
	for _, sessionKey := range s.SessionKeysForRecovery() {
		sessionKey = strings.TrimSpace(sessionKey)
		if sessionKey == "" {
			continue
		}
		if !s.SessionShouldStartNextSubmissionAsync(sessionKey) {
			continue
		}
		go s.StartNextSubmissionAsync(sessionKey, "codexRuntimeRecovered")
	}
}

// firstNonEmpty returns the first non-empty trimmed string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
