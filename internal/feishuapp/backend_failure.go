package feishuapp

import (
	retryview "feidex/internal/adapter/feishu/autoretry"
	mcpbridge "feidex/internal/adapter/feishu/mcpbridge"
	"feidex/internal/application/backendfailure"
	"feidex/internal/application/compaction"
	"feidex/internal/application/interaction"
	"feidex/internal/application/submission"
	domainsubmission "feidex/internal/domain/submission"
	backendruntime "feidex/internal/runtime"
	"feidex/internal/runtime/maintenance"

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

func failBackendActiveWork(d BackendRuntimeDeps, backend, scopeSessionKey, scopeThreadID, message string) {
	if d.store == nil {
		return
	}
	d.backendFailure.FailBackendActiveWork(backend, scopeSessionKey, scopeThreadID, message)
}

func failSubmissionWithoutTerminalCompletion(d BackendRuntimeDeps, sessionKey string, sub *domainsubmission.Submission, threadID, turnID, message string) {
	if d.store == nil || sub == nil {
		return
	}
	d.backendFailure.FailSubmissionWithoutTerminalCompletion(sessionKey, sub, threadID, turnID, message)
}

// BackendFailurePorts maps runtime and presentation effects to the failure use
// case. The services it calls back into are read once at construction: by the
// time composition builds the failure service every one of them is assigned,
// and a closure that reads a.bindings would hide that from the type system.
type BackendFailurePortInputs struct {
	Runtime              BackendRuntimeDeps
	TurnPresentation     *appturnstream.Service
	Compaction           *compaction.Service
	InteractionLifecycle interaction.LifecycleService
	AutoRetry            retryview.Service
	SubmissionCleanup    maintenance.SubmissionCleanup
	PendingQueue         *submission.PendingQueueService
	Submissions          *submission.SubmissionQueueService
	Cards                OutboundCardService
	AsyncRunner          func(func())
}

func BackendFailurePorts(inputs BackendFailurePortInputs) backendfailure.FailureDeps {
	runtimeDeps := inputs.Runtime
	owner := runtimeDeps.runtime.owner
	if owner == nil {
		return backendfailure.FailureDeps{}
	}
	stateStore := runtimeDeps.stateView
	return backendfailure.FailureDeps{
		Context: owner.Lifecycle.Context,
		State: backendfailure.FailureStateDeps{
			AllSessions: func() []*conversation.Session {
				return stateStore.Sessions()
			},
			GetSubmission: func(id string) *domainsubmission.Submission {
				return stateStore.Submission(id)
			},
			AllPendingRequests: func() []*state.PendingRequest {
				return stateStore.PendingRequests()
			},
			GetSession:     stateStore.Session,
			CommitTerminal: stateStore.CommitTerminal,
		},
		Sessions: backendfailure.FailureSessionDeps{
			SessionBelongsToFrontend: runtimeDeps.view.sessionBelongsToFrontend,
		},
		Runtime: backendfailure.FailureRuntimeDeps{
			RecordTurnError: func(threadID, turnID, message string) {
				inputs.TurnPresentation.RecordTurnError(threadID, turnID, message)
			},
			FlushTurnStream: func(ctx context.Context, threadID, turnID string) appturnstream.FlushResult {
				return inputs.TurnPresentation.FlushTurnStream(ctx, threadID, turnID)
			},
			FailStandaloneCompactTurn: func(threadID, turnID, message string) bool {
				return inputs.Compaction.FailStandaloneCompactTurn(threadID, turnID, message)
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
				inputs.InteractionLifecycle.ExpireAndPresent("claude", key, nil, "transport failure")
			},
			ObserveAutoRetryTerminal: func(sessionKey, threadID, status string, sess *conversation.Session, sub *domainsubmission.Submission, reuseMessageID, lastError string) bool {
				return inputs.AutoRetry.ObserveAutoRetryTerminal(sessionKey, threadID, status, sess, sub, reuseMessageID, lastError)
			},
			ReplaceTurnEventCard: func(ctx context.Context, sub *domainsubmission.Submission, title, color, body, eventType, threadID, reuseMessageID string) {
				inputs.Cards.replaceTurnEventCardWithReuse(ctx, sub, title, color, body, eventType, threadID, reuseMessageID)
			},
			PrependAttentionMention: func(text, userID string) string {
				return apputil.PrependAttentionMentionMarkdown(text, userID)
			},
			TurnStopAttentionUserID: func(sub *domainsubmission.Submission, turnID string) string {
				return turnStopAttentionUserID(stateStore, sub, turnID)
			},
		},
		Async: backendfailure.FailureAsyncDeps{
			CleanupSubmissionRuntimeState: func(sub *domainsubmission.Submission) {
				inputs.SubmissionCleanup.CleanupSubmissionRuntimeState(sub)
			},
			ClearSubmissionProcessingReactions: func(sub *domainsubmission.Submission) {
				inputs.PendingQueue.ClearSubmissionProcessingReactions(sub)
			},
			StartNextSubmissionAsync: func(sessionKey, reason string) {
				inputs.Submissions.StartNextSubmissionAsync(sessionKey, reason)
			},
			NextQueuedSubmissionSessionKey: func(sessionKey string) string {
				return inputs.Submissions.NextQueuedSessionKey(sessionKey)
			},
			RunAsync: func(fn func()) {
				owner.Lifecycle.Run(fn, inputs.AsyncRunner)
			},
			RunSessionAsync: func(sessionKey string, fn func()) {
				owner.Lifecycle.Run(func() { runSessionOnActor(runtimeDeps.sessionActors, sessionKey, fn) }, inputs.AsyncRunner)
			},
		},
	}
}
