package feishuapp

import (
	"context"
	"encoding/json"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	"strings"

	appcodexruntime "feidex/internal/runtime/codex"
)

// recoveryState belongs to one frontend; clients and recovery exclusion never cross runtimes.
func recoveryState(view runtimeView) *appcodexruntime.RecoveryState {
	owner := view.ensureRuntimeOwner()
	if owner == nil {
		return nil
	}
	return owner.CodexRecovery
}

// CodexRecoveryPorts takes the two entry points it needs from services that
// are constructed after it, so the recovery/upgrade group is a DAG.
func CodexRecoveryPorts(a *App,
	startVerifiedCodexClient func(context.Context) (appcodexruntime.CodexClient, error),
	recoverFrontend func(),
) appcodexruntime.RecoveryDependencies {
	submissions := a.bindings.Submissions
	return appcodexruntime.RecoveryDependencies{
		State:     recoveryState(a.runtimeView()),
		Context:   a.Context,
		RunAsync:  func(fn func()) { runAsync(a, fn) },
		ClearLive: func() { resetAppLiveThreadTracker(a) },
		FailActiveWork: func(cause error) {
			message := "Codex 后端异常退出。"
			if detail := strings.TrimSpace(errorText(cause)); detail != "" {
				message = "Codex 后端异常退出：" + detail
			}
			failBackendActiveWork(a.BackendRuntimeDeps(), domainbackend.BackendCodex, "", "", message)
		},
		StartVerifiedCodexClient: startVerifiedCodexClient,
		FrontendID: func() string {
			return a.frontendID
		},
		IsBackendActive: func() bool {
			return a.configView().configuredBackend() == domainbackend.BackendCodex
		},
		RecoverFrontendRuntimeState: recoverFrontend,
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
			submissions.StartNextSubmissionAsync(sessionKey, reason)
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

func replyCodexError(view runtimeView, requestID json.RawMessage, code int, message string) {
	if client := view.currentCodexClient(); client != nil {
		_ = client.ReplyError(requestID, code, message)
	}
}

func beginCodexAutoThreadRecoveryScope(codexrecovery appcodexruntime.RecoveryService) func() {
	return codexrecovery.BeginAutoThreadRecoveryScope()
}
