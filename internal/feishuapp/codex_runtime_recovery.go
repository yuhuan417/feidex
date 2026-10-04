package feishuapp

import (
	"context"
	"encoding/json"
	"strings"

	"feidex/internal/application/backendfailure"
	"feidex/internal/application/submission"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"

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

// CodexRecoveryPorts takes callbacks for owners constructed after recovery so
// the recovery/upgrade group remains a DAG.
type CodexRecoveryPortInputs struct {
	Runtime                  BackendRuntimeDeps
	Submissions              *submission.SubmissionQueueService
	AsyncRunner              func(func())
	BackendFailure           func() *backendfailure.BackendFailureService
	StartVerifiedCodexClient func(context.Context) (appcodexruntime.CodexClient, error)
	RecoverFrontendRuntime   func()
}

func CodexRecoveryPorts(inputs CodexRecoveryPortInputs) appcodexruntime.RecoveryDependencies {
	runtimeDeps := inputs.Runtime
	owner := runtimeDeps.runtime.owner
	if owner == nil {
		return appcodexruntime.RecoveryDependencies{}
	}
	stateStore := runtimeDeps.stateView
	liveThreads := owner.LiveThreads
	view := runtimeDeps.view
	return appcodexruntime.RecoveryDependencies{
		State:     recoveryState(runtimeDeps.runtime),
		Context:   owner.Lifecycle.Context,
		RunAsync:  func(fn func()) { owner.Lifecycle.Run(fn, inputs.AsyncRunner) },
		ClearLive: liveThreads.Reset,
		FailActiveWork: func(cause error) {
			message := "Codex 后端异常退出。"
			if detail := strings.TrimSpace(errorText(cause)); detail != "" {
				message = "Codex 后端异常退出：" + detail
			}
			if runtimeDeps.store == nil || inputs.BackendFailure == nil {
				return
			}
			if failure := inputs.BackendFailure(); failure != nil {
				failure.FailBackendActiveWork(domainbackend.BackendCodex, "", "", message)
			}
		},
		StartVerifiedCodexClient: inputs.StartVerifiedCodexClient,
		FrontendID: func() string {
			return runtimeDeps.frontendID
		},
		IsBackendActive: func() bool {
			return runtimeDeps.currentBackend().view.configuredBackend() == domainbackend.BackendCodex
		},
		RecoverFrontendRuntimeState: inputs.RecoverFrontendRuntime,
		SessionKeysForRecovery: func() []string {
			var keys []string
			for _, sess := range stateStore.Sessions() {
				if sess != nil && view.sessionBelongsToFrontend(sess.Key) {
					keys = append(keys, strings.TrimSpace(sess.Key))
				}
			}
			return keys
		},
		SessionShouldStartNextSubmissionAsync: func(sessionKey string) bool {
			sess := stateStore.Session(sessionKey)
			return conversation.ShouldStartNextSubmission(sess)
		},
		StartNextSubmissionAsync: func(sessionKey, reason string) {
			inputs.Submissions.StartNextSubmissionAsync(sessionKey, reason)
		},
		RunSessionAsync: func(sessionKey string, fn func()) {
			owner.Lifecycle.Run(func() { runSessionOnActor(runtimeDeps.sessionActors, sessionKey, fn) }, inputs.AsyncRunner)
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
