// Package codexruntime owns frontend-scoped Codex runtime recovery and upgrade.
package codexruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"feidex/internal/codexrpc"
)

// CodexClient is the protocol transport consumed by runtime supervision.
type CodexClient interface {
	SetHandlers(func(string, json.RawMessage), func(codexrpc.RequestEnvelope))
	Start(context.Context, bool) error
	Close() error
	Call(context.Context, string, any, any) error
	Reply(json.RawMessage, any) error
	ReplyError(json.RawMessage, int, string) error
}

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

func (s *RecoveryState) CurrentClient() CodexClient {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client
}
func (s *RecoveryState) ReplaceClient(next CodexClient) CodexClient {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.client
	s.client = next
	return previous
}

// RecoveryService manages Codex transport recovery. All host-app
// dependencies are injected as callback function fields.
type RecoveryDependencies struct {
	State   *RecoveryState
	Context func() context.Context

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

	// RunSessionAsync admits recovery continuations to the owning session actor.
	// Recovery itself remains frontend-wide, but queue mutations must preserve
	// the same ordering as ordinary turn completion callbacks.
	RunSessionAsync func(sessionKey string, fn func())
	RunAsync        func(func())
	ClearLive       func()
	FailActiveWork  func(error)
}

// NewRecoveryService creates a new RecoveryService.
type RecoveryService RecoveryDependencies

func NewRecoveryService(deps RecoveryDependencies) RecoveryService {
	return RecoveryService(deps)
}

// CurrentClient returns the current Codex client (thread-safe).
func (s RecoveryService) CurrentClient() CodexClient {
	return s.State.CurrentClient()
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
	return s.State.ReplaceClient(next)
}

func (s RecoveryService) HandleTransportFailure(client CodexClient, cause error) {
	if !s.BeginRecovery(client) {
		return
	}
	skip := s.AutoThreadRecoveryActive()
	s.ClearLive()
	s.RunAsync(func() { s.FailActiveWork(cause) })
	s.RunAsync(func() { s.RecoverAfterTransportFailure(client, skip) })
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

	parent := context.Background()
	if s.Context != nil {
		parent = s.Context()
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
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
	if s.SessionKeysForRecovery == nil || s.SessionShouldStartNextSubmissionAsync == nil || s.StartNextSubmissionAsync == nil || s.RunSessionAsync == nil {
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
		s.RunSessionAsync(sessionKey, func() {
			s.StartNextSubmissionAsync(sessionKey, "codexRuntimeRecovered")
		})
	}
}

// firstNonEmpty returns the first non-empty trimmed string.
