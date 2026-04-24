package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

func (a *App) beginCodexTransportRecovery(client codexClient) bool {
	if a == nil || client == nil {
		return false
	}
	a.codexRuntimeMu.Lock()
	defer a.codexRuntimeMu.Unlock()
	if runtime := backendRuntimeForKind(backendCodex); runtime == nil || !runtime.isActive(a) {
		return false
	}
	if a.codexRecovering || a.codex != client {
		return false
	}
	a.codexRecovering = true
	a.codexRecoverySource = client
	a.codex = nil
	return true
}

func codexRuntimeRecovering(a *App) bool {
	if a == nil {
		return false
	}
	a.codexRuntimeMu.Lock()
	defer a.codexRuntimeMu.Unlock()
	return a.codexRecovering
}

func currentCodexClient(a *App) codexClient {
	if a == nil {
		return nil
	}
	a.codexRuntimeMu.Lock()
	defer a.codexRuntimeMu.Unlock()
	return a.codex
}

func requireCodexClient(a *App) (codexClient, error) {
	client := currentCodexClient(a)
	if client == nil {
		return nil, fmt.Errorf("codex client not initialized")
	}
	return client, nil
}

func replaceCodexClient(a *App, next codexClient) codexClient {
	if a == nil {
		return nil
	}
	a.codexRuntimeMu.Lock()
	defer a.codexRuntimeMu.Unlock()
	prev := a.codex
	a.codex = next
	return prev
}

func replyCodexError(a *App, requestID json.RawMessage, code int, message string) {
	client := currentCodexClient(a)
	if client == nil {
		return
	}
	_ = client.ReplyError(requestID, code, message)
}

func (a *App) completeCodexTransportRecovery(next codexClient) bool {
	if a == nil {
		return false
	}
	a.codexRuntimeMu.Lock()
	defer a.codexRuntimeMu.Unlock()
	source := a.codexRecoverySource
	a.codexRecovering = false
	a.codexRecoverySource = nil
	if runtime := backendRuntimeForKind(backendCodex); runtime == nil || !runtime.isActive(a) {
		return false
	}
	if a.codex != nil && a.codex != source {
		return false
	}
	a.codex = next
	return next != nil
}

func beginCodexAutoThreadRecoveryScope(a *App) func() {
	if a == nil {
		return func() {}
	}
	a.codexAutoThreadMu.Lock()
	a.codexAutoThreading = true
	a.codexAutoThreadMu.Unlock()
	return func() {
		a.codexAutoThreadMu.Lock()
		a.codexAutoThreading = false
		a.codexAutoThreadMu.Unlock()
	}
}

func codexAutoThreadRecoveryActive(a *App) bool {
	if a == nil {
		return false
	}
	a.codexAutoThreadMu.Lock()
	defer a.codexAutoThreadMu.Unlock()
	return a.codexAutoThreading
}

func (a *App) recoverCodexRuntimeAfterTransportFailure(failed codexClient, skipFrontendRecovery bool) {
	if a == nil {
		return
	}
	if failed != nil {
		defer func() {
			if err := failed.Close(); err != nil {
				slog.Debug("codex transport failure close skipped",
					"frontend_id", a.frontendID,
					"error", err,
				)
			}
		}()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	next, err := newBackendUpgradeService(a).startVerifiedCodexClient(ctx)
	if err != nil {
		_ = a.completeCodexTransportRecovery(nil)
		slog.Error("codex runtime recovery failed",
			"frontend_id", a.frontendID,
			"error", err,
		)
		return
	}
	if !a.completeCodexTransportRecovery(next) {
		_ = next.Close()
		return
	}
	slog.Info("codex runtime recovered",
		"frontend_id", a.frontendID,
		"frontend_thread_recovery_skipped", skipFrontendRecovery,
	)
	if !skipFrontendRecovery {
		a.recoverFrontendRuntimeState()
	}
	a.resumeQueuedFrontendSessionsAfterCodexRecovery()
}

func (a *App) resumeQueuedFrontendSessionsAfterCodexRecovery() {
	if a == nil || a.store == nil {
		return
	}
	for _, sess := range appState(a).sessions() {
		if sess == nil || !sessionBelongsToFrontend(a, sess.Key) {
			continue
		}
		if !sessionShouldStartNextSubmissionAsync(sess) {
			continue
		}
		sessionKey := strings.TrimSpace(sess.Key)
		if sessionKey == "" {
			continue
		}
		go newLifecycleCoordinator(a).startNextSubmissionAsync(sessionKey, "codexRuntimeRecovered")
	}
}
