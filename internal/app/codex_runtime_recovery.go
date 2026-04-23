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
	if a.configuredBackend() != backendCodex {
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

func (a *App) codexRuntimeRecovering() bool {
	if a == nil {
		return false
	}
	a.codexRuntimeMu.Lock()
	defer a.codexRuntimeMu.Unlock()
	return a.codexRecovering
}

func (a *App) currentCodexClient() codexClient {
	if a == nil {
		return nil
	}
	a.codexRuntimeMu.Lock()
	defer a.codexRuntimeMu.Unlock()
	return a.codex
}

func (a *App) requireCodexClient() (codexClient, error) {
	client := a.currentCodexClient()
	if client == nil {
		return nil, fmt.Errorf("codex client not initialized")
	}
	return client, nil
}

func (a *App) replaceCodexClient(next codexClient) codexClient {
	if a == nil {
		return nil
	}
	a.codexRuntimeMu.Lock()
	defer a.codexRuntimeMu.Unlock()
	prev := a.codex
	a.codex = next
	return prev
}

func (a *App) replyCodexError(requestID json.RawMessage, code int, message string) {
	client := a.currentCodexClient()
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
	if a.configuredBackend() != backendCodex {
		return false
	}
	if a.codex != nil && a.codex != source {
		return false
	}
	a.codex = next
	return next != nil
}

func (a *App) recoverCodexRuntimeAfterTransportFailure(failed codexClient) {
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

	next, err := a.startVerifiedCodexClient(ctx)
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
	)
	a.recoverFrontendRuntimeState()
	a.resumeQueuedFrontendSessionsAfterCodexRecovery()
}

func (a *App) resumeQueuedFrontendSessionsAfterCodexRecovery() {
	if a == nil || a.store == nil {
		return
	}
	for _, sess := range a.appState().sessions() {
		if sess == nil || !a.sessionBelongsToFrontend(sess.Key) {
			continue
		}
		if !sessionShouldStartNextSubmissionAsync(sess) {
			continue
		}
		sessionKey := strings.TrimSpace(sess.Key)
		if sessionKey == "" {
			continue
		}
		go newSubmissionWorkflow(a).startNextSubmissionAsync(sessionKey, "codexRuntimeRecovered")
	}
}
