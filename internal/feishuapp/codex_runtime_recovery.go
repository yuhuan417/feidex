package feishuapp

import (
	"context"
	"encoding/json"
	codexadapter "feidex/internal/adapter/backend/codex"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	"strings"

	appcodexruntime "feidex/internal/runtime/codex"
)

// recoveryState belongs to one frontend; clients and recovery exclusion never cross runtimes.
func recoveryState(a *App) *appcodexruntime.RecoveryState {
	if a == nil {
		return nil
	}
	owner := a.runtimeView().ensureRuntimeOwner()
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
			failBackendActiveWork(a.BackendRuntimeDeps(), domainbackend.BackendCodex, "", "", message)
		},
		StartVerifiedCodexClient: func(ctx context.Context) (appcodexruntime.CodexClient, error) {
			return a.bindings.CodexUpgrade.StartVerifiedCodexClient(ctx)
		},
		FrontendID: func() string {
			return a.frontendID
		},
		IsBackendActive: func() bool {
			return a.configView().configuredBackend() == domainbackend.BackendCodex
		},
		RecoverFrontendRuntimeState: func() {
			recoverFrontendRuntimeState(a.bindings.StartupRecovery)
		},
		SessionKeysForRecovery: func() []string {
			var keys []string
			for _, sess := range a.State().Sessions() {
				if sess != nil && a.configView().sessionBelongsToFrontend(sess.Key) {
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

func codexRuntimeRecovering(codexrecovery appcodexruntime.RecoveryService) bool {
	return codexrecovery.IsRecovering()
}

func replaceCodexClient(codexrecovery appcodexruntime.RecoveryService, next CodexClient) CodexClient {
	return codexrecovery.ReplaceClient(next)
}

func replyCodexError(a *App, requestID json.RawMessage, code int, message string) {
	if client := a.runtimeView().currentCodexClient(); client != nil {
		_ = client.ReplyError(requestID, code, message)
	}
}

func beginCodexAutoThreadRecoveryScope(codexrecovery appcodexruntime.RecoveryService) func() {
	return codexrecovery.BeginAutoThreadRecoveryScope()
}

func requireCodexGateway(a *App) (codexadapter.Gateway, error) {
	client, err := a.runtimeView().requireCodexClient()
	return codexadapter.Gateway{Client: client}, err
}
