package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appcodexruntime "feidex/internal/app/codexruntime"
)

// codexRecoveryState is the app-level recovery state, lazily initialized.
var codexRecoveryState = appcodexruntime.NewRecoveryState()

// buildCodexRecoveryService builds a codexruntime.RecoveryService with
// all callbacks wired to *App dependencies. The StartVerifiedCodexClient
// callback is set separately to avoid circular initialization.
func buildCodexRecoveryService(a *App) appcodexruntime.RecoveryService {
	return appcodexruntime.RecoveryService{
		State: codexRecoveryState,
		FrontendID: func() string {
			return a.frontendID
		},
		IsBackendActive: func() bool {
			if runtime := backendRuntimeForKind(backendCodex); runtime != nil {
				return runtime.isActive(a)
			}
			return false
		},
		RecoverFrontendRuntimeState: func() {
			recoverFrontendRuntimeState(a)
		},
		SessionKeysForRecovery: func() []string {
			var keys []string
			for _, sess := range a.State().Sessions() {
				if sess != nil && sessionBelongsToFrontend(a, sess.Key) {
					keys = append(keys, strings.TrimSpace(sess.Key))
				}
			}
			return keys
		},
		SessionShouldStartNextSubmissionAsync: func(sessionKey string) bool {
			sess := a.State().Session(sessionKey)
			return sessionShouldStartNextSubmissionAsync(sess)
		},
		StartNextSubmissionAsync: func(sessionKey, reason string) {
			newSubmissionQueueServiceFromApp(a).StartNextSubmissionAsync(sessionKey, reason)
		},
	}
}

func beginCodexTransportRecovery(a *App, client CodexClient) bool {
	return buildCodexRecoveryService(a).BeginRecovery(client)
}

func codexRuntimeRecovering(a *App) bool {
	return buildCodexRecoveryService(a).IsRecovering()
}

func getCodex(a *App) CodexClient {
	if a == nil {
		return nil
	}
	a.codexRuntimeMu.Lock()
	defer a.codexRuntimeMu.Unlock()
	return a.codex
}

func setCodex(a *App, c CodexClient) {
	if a == nil {
		return
	}
	a.codexRuntimeMu.Lock()
	a.codex = c
	a.codexRuntimeMu.Unlock()
}

func currentCodexClient(a *App) CodexClient {
	if a == nil {
		return nil
	}
	svc := buildCodexRecoveryService(a)
	if svc.IsRecovering() {
		return svc.CurrentClient()
	}
	if c := getCodex(a); c != nil {
		return c
	}
	return svc.CurrentClient()
}

func requireCodexClient(a *App) (CodexClient, error) {
	client := currentCodexClient(a)
	if client == nil {
		return nil, fmt.Errorf("codex client not initialized")
	}
	return client, nil
}

func replaceCodexClient(a *App, next CodexClient) CodexClient {
	prev := buildCodexRecoveryService(a).ReplaceClient(next)
	setCodex(a, next)
	return prev
}

func replyCodexError(a *App, requestID json.RawMessage, code int, message string) {
	buildCodexRecoveryService(a).ReplyError(requestID, code, message)
}

func completeCodexTransportRecovery(a *App, next CodexClient) bool {
	if ok := buildCodexRecoveryService(a).CompleteRecovery(next); ok {
		setCodex(a, next)
		return true
	}
	return false
}

func beginCodexAutoThreadRecoveryScope(a *App) func() {
	return buildCodexRecoveryService(a).BeginAutoThreadRecoveryScope()
}

func codexAutoThreadRecoveryActive(a *App) bool {
	return buildCodexRecoveryService(a).AutoThreadRecoveryActive()
}

func recoverCodexRuntimeAfterTransportFailure(a *App, failed CodexClient, skipFrontendRecovery bool) {
	svc := buildCodexRecoveryService(a)
	svc.StartVerifiedCodexClient = func(ctx context.Context) (appcodexruntime.CodexClient, error) {
		return newBackendUpgradeService(a).startVerifiedCodexClient(ctx)
	}
	svc.RecoverAfterTransportFailure(failed, skipFrontendRecovery)
	if a != nil && !svc.IsRecovering() {
		if next := svc.CurrentClient(); next != nil {
			setCodex(a, next)
		}
	}
}

func resumeQueuedFrontendSessionsAfterCodexRecovery(a *App) {
	if a == nil || a.store == nil {
		return
	}
	buildCodexRecoveryService(a).ResumeQueuedSessions()
}
