package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	appbackend "feidex/internal/app/backend"
	appsessionctx "feidex/internal/app/sessionctx"
	appturnlifecycle "feidex/internal/app/turnlifecycle"
	appturnstream "feidex/internal/app/turnstream"
	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

type codexErrorAware interface {
	SetErrorHandler(func(error))
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

func backendFailureScopeMatches(sess *state.Session, scopeSessionKey, scopeThreadID string) bool {
	return appbackend.BackendFailureScopeMatches(sess, scopeSessionKey, scopeThreadID)
}

func resolvePendingRequestsForTerminalFailure(a *App, sessionKey, threadID, turnID string) {
	if a == nil || a.store == nil {
		return
	}
	newBackendFailureService(a).ResolvePendingRequestsForTerminalFailure(sessionKey, threadID, turnID)
}

func failSubmissionWithoutTerminalCompletion(a *App, sessionKey string, sub *state.Submission, threadID, turnID, message string) {
	if a == nil || a.store == nil || sub == nil {
		return
	}
	appState := a.State()
	current := appState.Submission(sub.ID)
	if current == nil || current.Finalized {
		return
	}
	sub = current
	threadID = firstNonEmpty(strings.TrimSpace(threadID), strings.TrimSpace(sub.ThreadID))
	turnID = firstNonEmpty(strings.TrimSpace(turnID), strings.TrimSpace(sub.TurnID))
	if turnID != "" && message != "" {
		newTurnStreamService(a).recordTurnError(threadID, turnID, message)
	}
	flush := appturnstream.FlushResult{}
	if turnID != "" {
		flush = newTurnStreamService(a).flushTurnStream(context.Background(), threadID, turnID)
	}
	newBackendFailureService(a).ResolvePendingRequestsForTerminalFailure(sessionKey, threadID, turnID)
	_ = appState.FinalizeSubmission(sub.ID, state.SubmissionStatusFailed.String())
	sub = appState.Submission(sub.ID)
	if sub == nil {
		return
	}
	newPendingQueueService(a).clearSubmissionProcessingReactions(sub)
	terminalText := appturnlifecycle.TurnCompletionTerminalText(sub.Status, firstNonEmpty(strings.TrimSpace(message), strings.TrimSpace(flush.LastError)))
	reuseMessageID := strings.TrimSpace(flush.WorkingMessageID)
	updatedSess, _ := appState.UpdateSession(sessionKey, func(sess *state.Session) {
		if sess == nil {
			return
		}
		appsessionctx.RemoveActiveOperation(sess, sub.ID, turnID)
		switch {
		case appsessionctx.HasActiveOperations(sess):
			sess.Status = state.SessionStatusTurnStarting.String()
			for _, op := range sess.ActiveOperations {
				if strings.TrimSpace(op.TurnID) != "" {
					sess.Status = state.SessionStatusTurnInProgress.String()
					break
				}
			}
		case len(sess.Queue) > 0 || len(sess.StagedImages) > 0:
			sess.Status = state.SessionStatusQueued.String()
		default:
			sess.Status = state.SessionStatusIdle.String()
		}
	})
	suppressTerminalCard := false
	if updatedSess != nil {
		suppressTerminalCard = newAutoRetryService(a).ObserveAutoRetryTerminal(sessionKey, threadID, "failed", updatedSess, sub, reuseMessageID)
	}
	if terminalText != "" && !suppressTerminalCard {
		newOutboundCardService(a).replaceTurnEventCardWithReuse(
			context.Background(),
			sub,
			"任务状态",
			"grey",
			prependAttentionMentionMarkdown(terminalText, turnStopAttentionUserID(a, sub, turnID)),
			"turn_terminal",
			"",
			reuseMessageID,
		)
	}
	newRuntimeMaintenanceService(a).CleanupSubmissionRuntimeState(sub)
	if updatedSess != nil && sessionShouldStartNextSubmissionAsync(updatedSess) {
		runAsync(a, func() {
			newSubmissionQueueServiceFromApp(a).StartNextSubmissionAsync(sessionKey, "backendFailed")
		})
	}
}

// newBackendFailureService builds a backend.BackendFailureService with
// all callbacks wired to *App dependencies.
func newBackendFailureService(a *App) appbackend.BackendFailureService {
	return appbackend.NewBackendFailureService(appbackend.FailureDeps{
		App: a,
		State: appbackend.FailureStateDeps{
			AllSessions: func() []*state.Session {
				return a.State().Sessions()
			},
			GetSubmission: func(id string) *state.Submission {
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
			UpdateSession: func(key string, mutate func(*state.Session)) (*state.Session, error) {
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
				return failStandaloneCompactTurn(a, threadID, turnID, message)
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
			ObserveAutoRetryTerminal: func(sessionKey, threadID, status string, sess *state.Session, sub *state.Submission, reuseMessageID string) bool {
				return newAutoRetryService(a).ObserveAutoRetryTerminal(sessionKey, threadID, status, sess, sub, reuseMessageID)
			},
			ReplaceTurnEventCard: func(ctx context.Context, sub *state.Submission, title, color, body, eventType, threadID, reuseMessageID string) {
				newOutboundCardService(a).replaceTurnEventCardWithReuse(ctx, sub, title, color, body, eventType, threadID, reuseMessageID)
			},
			PrependAttentionMention: func(text, userID string) string {
				return prependAttentionMentionMarkdown(text, userID)
			},
			TurnStopAttentionUserID: func(sub *state.Submission, turnID string) string {
				return turnStopAttentionUserID(a, sub, turnID)
			},
		},
		Async: appbackend.FailureAsyncDeps{
			CleanupSubmissionRuntimeState: func(sub *state.Submission) {
				newRuntimeMaintenanceService(a).CleanupSubmissionRuntimeState(sub)
			},
			StartNextSubmissionAsync: func(sessionKey, reason string) {
				newSubmissionQueueServiceFromApp(a).StartNextSubmissionAsync(sessionKey, reason)
			},
			RunAsync: func(fn func()) {
				runAsync(a, fn)
			},
		},
	})
}
