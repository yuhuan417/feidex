package feishuapp

import (
	mcpbridge "feidex/internal/adapter/feishu/mcpbridge"
	"feidex/internal/application/backendfailure"
	domainsubmission "feidex/internal/domain/submission"
	backendruntime "feidex/internal/runtime"

	"context"
	"encoding/json"
	"feidex/internal/domain/conversation"
	apputil "feidex/internal/formatutil"
	"log/slog"

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

func configureCodexClientRuntime(d BackendRuntimeDeps, client CodexClient) {
	if client == nil {
		return
	}
	client.SetHandlers(
		func(method string, params json.RawMessage) { handleNotification(d, method, params) },
		func(req codexrpc.RequestEnvelope) { handleServerRequest(d, req) },
	)
	if aware, ok := client.(codexErrorAware); ok {
		aware.SetErrorHandler(func(err error) {
			handleCodexTransportError(d, client, err)
		})
	}
	publishMCPToCodexClient(d, client)
}

func publishMCPToCodexClient(d BackendRuntimeDeps, client CodexClient) {
	aware, ok := client.(codexMCPAware)
	if !ok {
		return
	}
	pub := currentMCPPublicationFor(d.runtime.ensureRuntimeOwner(), d.mcp)
	aware.SetMCPServerPublication(mcpbridge.ServerID, pub.URL, mcpbridge.BearerEnvName, pub.Token)
}

func handleCodexTransportError(d BackendRuntimeDeps, client CodexClient, err error) {
	slog.Error("codex backend transport failed",
		"frontend_id", d.frontendID,
		"error", err,
	)
	d.codexRecovery.HandleTransportFailure(client, err)
}

func failClaudeSessionActiveWork(backendfailureDep *backendfailure.BackendFailureService, sessionKey, threadID string, err error) {
	backendfailureDep.FailClaudeSessionActiveWork(sessionKey, threadID, err)
}

func errorText(err error) string {
	return backendfailure.ErrorText(err)
}

func failBackendActiveWork(a *App, backend, scopeSessionKey, scopeThreadID, message string) {
	if a == nil || a.store == nil {
		return
	}
	a.bindings.BackendFailure.FailBackendActiveWork(backend, scopeSessionKey, scopeThreadID, message)
}

func failSubmissionWithoutTerminalCompletion(a *App, sessionKey string, sub *domainsubmission.Submission, threadID, turnID, message string) {
	if a == nil || a.store == nil || sub == nil {
		return
	}
	a.bindings.BackendFailure.FailSubmissionWithoutTerminalCompletion(sessionKey, sub, threadID, turnID, message)
}

// BackendFailurePorts maps runtime and presentation effects to the failure use case.
func BackendFailurePorts(a *App) backendfailure.FailureDeps {
	return backendfailure.FailureDeps{
		Context: a.Context,
		State: backendfailure.FailureStateDeps{
			AllSessions: func() []*conversation.Session {
				return a.State().Sessions()
			},
			GetSubmission: func(id string) *domainsubmission.Submission {
				return a.State().Submission(id)
			},
			AllPendingRequests: func() []*state.PendingRequest {
				return a.State().PendingRequests()
			},
			GetSession:     a.State().Session,
			CommitTerminal: a.State().CommitTerminal,
		},
		Sessions: backendfailure.FailureSessionDeps{
			SessionBelongsToFrontend: func(sessionKey string) bool {
				return a.configView().sessionBelongsToFrontend(sessionKey)
			},
		},
		Runtime: backendfailure.FailureRuntimeDeps{
			RecordTurnError: func(threadID, turnID, message string) {
				a.bindings.TurnPresentation.RecordTurnError(threadID, turnID, message)
			},
			FlushTurnStream: func(ctx context.Context, threadID, turnID string) appturnstream.FlushResult {
				return a.bindings.TurnPresentation.FlushTurnStream(ctx, threadID, turnID)
			},
			FailStandaloneCompactTurn: func(threadID, turnID, message string) bool {
				return a.bindings.Compaction.FailStandaloneCompactTurn(threadID, turnID, message)
			},
			BackendRuntimeFailsStandaloneCompaction: func(backend string) bool {
				if runtime := backendruntime.BackendForKind(backend); runtime != nil {
					return runtime.FailsStandaloneCompaction()
				}
				return false
			},
		},
		Cards: backendfailure.FailureCardDeps{
			ExpireClaudeInteractions: func(key string) {
				a.bindings.InteractionLifecycle.ExpireAndPresent("claude", key, nil, "transport failure")
			},
			ObserveAutoRetryTerminal: func(sessionKey, threadID, status string, sess *conversation.Session, sub *domainsubmission.Submission, reuseMessageID, lastError string) bool {
				return a.bindings.AutoRetry.ObserveAutoRetryTerminal(sessionKey, threadID, status, sess, sub, reuseMessageID, lastError)
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
		Async: backendfailure.FailureAsyncDeps{
			CleanupSubmissionRuntimeState: func(sub *domainsubmission.Submission) {
				a.bindings.SubmissionCleanup.CleanupSubmissionRuntimeState(sub)
			},
			ClearSubmissionProcessingReactions: func(sub *domainsubmission.Submission) {
				a.bindings.PendingQueue.ClearSubmissionProcessingReactions(sub)
			},
			StartNextSubmissionAsync: func(sessionKey, reason string) {
				a.bindings.Submissions.StartNextSubmissionAsync(sessionKey, reason)
			},
			NextQueuedSubmissionSessionKey: func(sessionKey string) string {
				return a.bindings.Submissions.NextQueuedSessionKey(sessionKey)
			},
			RunAsync: func(fn func()) {
				runAsync(a, fn)
			},
			RunSessionAsync: func(sessionKey string, fn func()) {
				runSessionAsync(a, sessionKey, fn)
			},
		},
	}
}
