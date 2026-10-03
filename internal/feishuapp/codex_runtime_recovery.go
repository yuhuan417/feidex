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
	return owner.CodexRecovery
}

func CodexRecoveryPorts(a *App) appcodexruntime.RecoveryDependencies {
	return appcodexruntime.RecoveryDependencies{
		State:     recoveryState(a),
		Context:   a.Context,
		RunAsync:  func(fn func()) { runAsync(a, fn) },
		ClearLive: func() { resetLiveThreadState(a) },
		FailActiveWork: func(cause error) {
			message := "Codex 后端异常退出。"
			if detail := strings.TrimSpace(errorText(cause)); detail != "" {
				message = "Codex 后端异常退出：" + detail
			}
			failBackendActiveWork(a, domainbackend.BackendCodex, "", "", message)
		},
		StartVerifiedCodexClient: func(ctx context.Context) (appcodexruntime.CodexClient, error) {
			return a.bindings.CodexUpgrade.StartVerifiedCodexClient(ctx)
		},
		FrontendID: func() string {
			return a.frontendID
		},
		IsBackendActive: func() bool {
			return configuredBackend(a) == domainbackend.BackendCodex
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
			a.bindings.Submissions.StartNextSubmissionAsync(sessionKey, reason)
		},
		RunSessionAsync: func(sessionKey string, fn func()) {
			runSessionAsync(a, sessionKey, fn)
		},
	}
}

func codexRuntimeRecovering(a *App) bool {
	return a.bindings.CodexRecovery.IsRecovering()
}

func getCodex(a *App) CodexClient {
	if a == nil {
		return nil
	}
	return ensureRuntimeOwner(a).CodexClient()
}

func setCodex(a *App, c CodexClient) {
	if a == nil {
		return
	}
	ensureRuntimeOwner(a).SetCodexClient(c)
}

func currentCodexClient(a *App) CodexClient {
	if a == nil {
		return nil
	}
	return getCodex(a)
}

func requireCodexClient(a *App) (CodexClient, error) {
	client := currentCodexClient(a)
	if client == nil {
		return nil, fmt.Errorf("codex client not initialized")
	}
	return client, nil
}

func replaceCodexClient(a *App, next CodexClient) CodexClient {
	return a.bindings.CodexRecovery.ReplaceClient(next)
}

func replyCodexError(a *App, requestID json.RawMessage, code int, message string) {
	if client := currentCodexClient(a); client != nil {
		_ = client.ReplyError(requestID, code, message)
	}
}

func beginCodexAutoThreadRecoveryScope(a *App) func() {
	return a.bindings.CodexRecovery.BeginAutoThreadRecoveryScope()
}

func requireCodexGateway(a *App) (codexadapter.Gateway, error) {
	client, err := requireCodexClient(a)
	return codexadapter.Gateway{Client: client}, err
}
