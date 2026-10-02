package app

import (
	domainsubmission "feidex/internal/domain/submission"

	"context"
	"encoding/json"
	"feidex/internal/domain/conversation"
	apputil "feidex/internal/formatutil"
	"log/slog"
	"strings"

	appbackend "feidex/internal/app/backend"

	appturnstream "feidex/internal/adapter/feishu/turnstream"
	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

type codexErrorAware interface {
	SetErrorHandler(func(error))
}

type codexMCPAware interface {
	SetMCPServerPublication(name, url, bearerTokenEnvVar, bearerToken string)
}

func configureCodexClientRuntime(a *App, client CodexClient) {
	if client == nil {
		return
	}
	client.SetHandlers(
		func(method string, params json.RawMessage) { handleNotification(a, method, params) },
		func(req codexrpc.RequestEnvelope) { handleServerRequest(a, req) },
	)
	if aware, ok := client.(codexErrorAware); ok {
		aware.SetErrorHandler(func(err error) {
			a.handleCodexTransportError(client, err)
		})
	}
	publishMCPToCodexClient(a, client)
}

func publishMCPToCodexClient(a *App, client CodexClient) {
	aware, ok := client.(codexMCPAware)
	if !ok {
		return
	}
	pub := currentMCPPublication(a)
	aware.SetMCPServerPublication(feidexMCPServerID, pub.URL, feidexMCPBearerEnvName, pub.Token)
}

func (a *App) handleCodexTransportError(client CodexClient, err error) {
	if a == nil || !beginCodexTransportRecovery(a, client) {
		return
	}
	skipFrontendRecovery := codexAutoThreadRecoveryActive(a)
	message := "Codex 后端异常退出。"
	if detail := strings.TrimSpace(errorText(err)); detail != "" {
		message = "Codex 后端异常退出：" + detail
	}
	slog.Error("codex backend transport failed",
		"frontend_id", a.frontendID,
		"error", err,
	)
	resetLiveThreadState(a)
	runAsync(a, func() {
		failBackendActiveWork(a, backendCodex, "", "", message)
	})
	runAsync(a, func() {
		recoverCodexRuntimeAfterTransportFailure(a, client, skipFrontendRecovery)
	})
}

func failClaudeSessionActiveWork(a *App, sessionKey, threadID string, err error) {
	// The Claude runtime just reported that the session broke. Close out the
	// cards that were still waiting for an answer before the terminal-failure
	// sweep marks them resolved, otherwise they stay clickable and fail later.
	ExpireClaudeInteractionCards(a, sessionKey, nil, "transport failure")
	newBackendFailureService(a).FailClaudeSessionActiveWork(sessionKey, threadID, err)
}

func errorText(err error) string {
	return appbackend.ErrorText(err)
}

func failBackendActiveWork(a *App, backend, scopeSessionKey, scopeThreadID, message string) {
	if a == nil || a.store == nil {
		return
	}
	newBackendFailureService(a).FailBackendActiveWork(backend, scopeSessionKey, scopeThreadID, message)
}

func failSubmissionWithoutTerminalCompletion(a *App, sessionKey string, sub *domainsubmission.Submission, threadID, turnID, message string) {
	if a == nil || a.store == nil || sub == nil {
		return
	}
	newBackendFailureService(a).FailSubmissionWithoutTerminalCompletion(sessionKey, sub, threadID, turnID, message)
}

// newBackendFailureService builds a backend.BackendFailureService with
// all callbacks wired to *App dependencies.
func newBackendFailureService(a *App) appbackend.BackendFailureService {
	return appbackend.NewBackendFailureService(appbackend.FailureDeps{
		App: a,
		State: appbackend.FailureStateDeps{
			AllSessions: func() []*conversation.Session {
				return a.State().Sessions()
			},
			GetSubmission: func(id string) *domainsubmission.Submission {
				return a.State().Submission(id)
			},
			AllPendingRequests: func() []*state.PendingRequest {
				return a.State().PendingRequests()
			},
			UpdatePending: func(id string, mutate func(*state.PendingRequest)) error {
				return a.State().UpdatePending(id, mutate)
			},
			FinalizeSubmission: func(id, status string) error {
				return a.State().FinalizeSubmission(id, status)
			},
			UpdateSession: func(key string, mutate func(*conversation.Session)) (*conversation.Session, error) {
				return a.State().UpdateSession(key, mutate)
			},
		},
		Sessions: appbackend.FailureSessionDeps{
			SessionBelongsToFrontend: func(sessionKey string) bool {
				return sessionBelongsToFrontend(a, sessionKey)
			},
		},
		Runtime: appbackend.FailureRuntimeDeps{
			RecordTurnError: func(threadID, turnID, message string) {
				newTurnStreamService(a).recordTurnError(threadID, turnID, message)
			},
			FlushTurnStream: func(ctx context.Context, threadID, turnID string) appturnstream.FlushResult {
				return newTurnStreamService(a).flushTurnStream(ctx, threadID, turnID)
			},
			FailStandaloneCompactTurn: func(threadID, turnID, message string) bool {
				return newCompactionService(a).FailStandaloneCompactTurn(threadID, turnID, message)
			},
			BackendRuntimeFailsStandaloneCompaction: func(backend string) bool {
				if runtime := backendRuntimeForKind(backend); runtime != nil {
					return runtime.failsStandaloneCompaction()
				}
				return false
			},
			BackendRuntimeHandleTransportFailure: func(backend, sessionKey, threadID string, err error) {
				if runtime := backendRuntimeForKind(backend); runtime != nil {
					runtime.handleTransportFailure(a, sessionKey, threadID, err)
				}
			},
		},
		Cards: appbackend.FailureCardDeps{
			ObserveAutoRetryTerminal: func(sessionKey, threadID, status string, sess *conversation.Session, sub *domainsubmission.Submission, reuseMessageID, lastError string) bool {
				return newAutoRetryService(a).ObserveAutoRetryTerminal(sessionKey, threadID, status, sess, sub, reuseMessageID, lastError)
			},
			ReplaceTurnEventCard: func(ctx context.Context, sub *domainsubmission.Submission, title, color, body, eventType, threadID, reuseMessageID string) {
				newOutboundCardService(a).replaceTurnEventCardWithReuse(ctx, sub, title, color, body, eventType, threadID, reuseMessageID)
			},
			PrependAttentionMention: func(text, userID string) string {
				return apputil.PrependAttentionMentionMarkdown(text, userID)
			},
			TurnStopAttentionUserID: func(sub *domainsubmission.Submission, turnID string) string {
				return turnStopAttentionUserID(a, sub, turnID)
			},
		},
		Async: appbackend.FailureAsyncDeps{
			CleanupSubmissionRuntimeState: func(sub *domainsubmission.Submission) {
				newSubmissionCleanup(a).CleanupSubmissionRuntimeState(sub)
			},
			ClearSubmissionProcessingReactions: func(sub *domainsubmission.Submission) {
				newPendingQueueService(a).clearSubmissionProcessingReactions(sub)
			},
			StartNextSubmissionAsync: func(sessionKey, reason string) {
				newSubmissionQueueServiceFromApp(a).StartNextSubmissionAsync(sessionKey, reason)
			},
			NextQueuedSubmissionSessionKey: func(sessionKey string) string {
				return newSubmissionQueueServiceFromApp(a).NextQueuedSessionKey(sessionKey)
			},
			RunAsync: func(fn func()) {
				runAsync(a, fn)
			},
		},
	})
}
