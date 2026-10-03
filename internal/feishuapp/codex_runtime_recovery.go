package feishuapp

import (
	"context"
	"encoding/json"
	codexadapter "feidex/internal/adapter/backend/codex"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	"fmt"
	"strings"

	appcodexruntime "feidex/internal/runtime/codex"
)

// recoveryState belongs to one frontend; clients and recovery exclusion never cross runtimes.
func recoveryState(a *App) *appcodexruntime.RecoveryState {
	if a == nil {
		return nil
	}
	owner := ensureRuntimeOwner(a)
	if owner.CodexRecovery == nil {
		owner.CodexRecovery = appcodexruntime.NewRecoveryState()
	}
	return owner.CodexRecovery
}

// buildCodexRecoveryService builds a codexruntime.RecoveryService with
// all callbacks wired to *App dependencies. The StartVerifiedCodexClient
// callback is set separately to avoid circular initialization.
func buildCodexRecoveryService(a *App) appcodexruntime.RecoveryService {
	return appcodexruntime.RecoveryService{
		State:   recoveryState(a),
		Context: a.Context,
		FrontendID: func() string {
			return a.frontendID
		},
		IsBackendActive: func() bool {
			if runtime := backendRuntimeForKind(domainbackend.BackendCodex); runtime != nil {
				return runtime.isActive(backendRuntimeContextForApp(a))
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
			return conversation.ShouldStartNextSubmission(sess)
		},
		StartNextSubmissionAsync: func(sessionKey, reason string) {
			newSubmissionQueueServiceFromApp(a).StartNextSubmissionAsync(sessionKey, reason)
		},
		RunSessionAsync: func(sessionKey string, fn func()) {
			runSessionAsync(a, sessionKey, fn)
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
	owner := ensureRuntimeOwner(a)
	registryFor(a).ClientsMu.RLock()
	client, _ := registryFor(a).Codex.(CodexClient)
	registryFor(a).ClientsMu.RUnlock()
	if current := owner.CodexClient(); current != nil {
		return current
	}
	return client
}

func setCodex(a *App, c CodexClient) {
	if a == nil {
		return
	}
	registryFor(a).ClientsMu.Lock()
	registryFor(a).Codex = c
	registryFor(a).ClientsMu.Unlock()
	ensureRuntimeOwner(a).SetCodexClient(c)
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
	if client := currentCodexClient(a); client != nil {
		_ = client.ReplyError(requestID, code, message)
	}
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

func requireCodexGateway(a *App) (codexadapter.Gateway, error) {
	client, err := requireCodexClient(a)
	return codexadapter.Gateway{Client: client}, err
}
