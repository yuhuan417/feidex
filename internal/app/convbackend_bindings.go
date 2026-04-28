package app

import (
	"context"
	"fmt"
	"strings"

	appcore "feidex/internal/app/appcore"
	appconvbackend "feidex/internal/app/convbackend"
	appdebugviewcmd "feidex/internal/app/debugviewcmd"
	apphistorycmd "feidex/internal/app/historycmd"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// ConversationProvider adapter
// ---------------------------------------------------------------------------

type convBackendConversationAdapter struct{}

func (a convBackendConversationAdapter) ListCodexThreads(app appconvbackend.App, sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return newWorkspaceThreadServiceInner(app.(*App)).ListCodexWorkspaceThreads(sessionKey, ws, includeAll)
}

func (a convBackendConversationAdapter) EnsureCodexBinding(app appconvbackend.App, sessionKey string, sess *state.Session, ws *config.Workspace) (*appconvbackend.ThreadBinding, error) {
	return newWorkspaceThreadServiceInner(app.(*App)).EnsureCodexWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) StartCodexThread(app appconvbackend.App, sessionKey string, sess *state.Session, ws *config.Workspace) (*appconvbackend.ThreadBinding, error) {
	return newWorkspaceThreadServiceInner(app.(*App)).StartCodexWorkspaceThread(sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) ResumeCodexThread(app appconvbackend.App, sessionKey string, sess *state.Session, ws *config.Workspace, sel appconvbackend.ThreadResumeSelection) (*appconvbackend.ThreadBinding, error) {
	root := app.(*App)
	return appconvbackend.ResumeCodexSelectedThread(appconvbackend.CodexResumeDeps{
		RequireClient: func() (appconvbackend.CodexRPCClient, error) { return requireCodexClient(root) },
		SaveSession:   root.State().SaveSession,
		SetThreadContext: func(sess *state.Session, workspaceID, threadID, name, preview string) {
			setSessionThreadContext(sess, workspaceID, threadID, name, preview)
		},
		ResetActiveOps:     sessionResetActiveOperations,
		MarkThreadLive:     func(sessionKey, threadID string) { markSessionThreadLive(root, sessionKey, threadID) },
		DefaultWorkspaceID: func() string { return defaultWorkspaceID(root) },
		ConfiguredModel:    func() string { return configuredGlobalModel(root.cfg) },
	}, sessionKey, sess, ws, sel)
}

func (a convBackendConversationAdapter) InterruptCodexTurn(app appconvbackend.App, ctx context.Context, sess *state.Session) error {
	return appconvbackend.InterruptCodexActiveTurn(appconvbackend.CodexInterruptDeps{
		RequireClient: func() (appconvbackend.CodexRPCClient, error) { return requireCodexClient(app.(*App)) },
	}, ctx, sess)
}

func (a convBackendConversationAdapter) ContinueCodexTurn(app appconvbackend.App, sessionKey, text string) error {
	root := app.(*App)
	return appconvbackend.ContinueCodexActiveTurn(appconvbackend.CodexContinueDeps{
		RequireClient: func() (appconvbackend.CodexRPCClient, error) { return requireCodexClient(root) },
		GetSession:    root.State().Session,
	}, sessionKey, text)
}

func (a convBackendConversationAdapter) TryCodexReplyContinuation(app appconvbackend.App, msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	root := app.(*App)
	replySvc := newReplyContinuationService(root)
	return appconvbackend.TryCodexReplyContinuation(appconvbackend.CodexReplyContinuationDeps{
		RequireClient: func() (appconvbackend.CodexRPCClient, error) { return requireCodexClient(root) },
		ResolveInboundAttachments: func(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]state.SubmissionAttachment, error) {
			return resolveInboundAttachments(root, msg, workspaceID, sessionKey)
		},
		PendingInputSessionKey:     replySvc.pendingInputSessionKey,
		CollectPendingStagedImages: replySvc.collectPendingStagedImages,
		ClearPendingStagedImages:   replySvc.clearPendingStagedImages,
		BuildTurnInputs:            buildTurnInputs,
		SaveSession:                root.State().SaveSession,
		DefaultWorkspaceID:         func() string { return defaultWorkspaceID(root) },
	}, msg, link, sessionKey, sess)
}

func (a convBackendConversationAdapter) ForkCodexConversation(app appconvbackend.App, sessionKey string, sess *state.Session, ws *config.Workspace) (string, error) {
	return forkCodexActiveConversation(app.(*App), sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) RecoverCodexStartup(app appconvbackend.App, sessionKey, workspaceID string, sess *state.Session, ws *config.Workspace, effectiveModel string) {
	root := app.(*App)
	appconvbackend.RecoverCodexStartupConversation(appconvbackend.CodexStartupRecoveryDeps{
		CurrentClient: func() appconvbackend.CodexRPCClient { return currentCodexClient(root) },
		RuntimeRecovering: func() bool {
			return codexRuntimeRecovering(root)
		},
		BuildThreadStartParams: func(ws *config.Workspace, sess *state.Session, effectiveModel string) codexrpc.ThreadStartParams {
			return buildThreadStartParams(root, ws, sess, effectiveModel)
		},
		SaveSession: root.State().SaveSession,
		SetThreadContext: func(sess *state.Session, workspaceID, threadID, name, preview string) {
			setSessionThreadContext(sess, workspaceID, threadID, name, preview)
		},
		ClearThreadContext:     clearSessionThreadContext,
		MarkThreadLive:         func(sessionKey, threadID string) { markSessionThreadLive(root, sessionKey, threadID) },
		ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(root, sessionKey) },
	}, sessionKey, workspaceID, sess, ws, effectiveModel)
}

func (a convBackendConversationAdapter) ListClaudeThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return listClaudeSessions(sessionKey, ws, includeAll)
}

func (a convBackendConversationAdapter) EnsureClaudeBinding(app appconvbackend.App, sessionKey string, sess *state.Session, ws *config.Workspace) (*appconvbackend.ThreadBinding, error) {
	return newWorkspaceThreadServiceInner(app.(*App)).EnsureClaudeWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) StartClaudeThread(app appconvbackend.App, sessionKey string, sess *state.Session, ws *config.Workspace) (*appconvbackend.ThreadBinding, error) {
	return newWorkspaceThreadServiceInner(app.(*App)).StartClaudeWorkspaceThread(sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) ResumeClaudeThread(app appconvbackend.App, sessionKey string, sess *state.Session, ws *config.Workspace, sel appconvbackend.ThreadResumeSelection) (*appconvbackend.ThreadBinding, error) {
	root := app.(*App)
	return appconvbackend.ResumeClaudeSelectedThread(appconvbackend.ClaudeResumeDeps{
		FindSessionEntry: findClaudeSessionEntry,
		EnsureSession:    root.claude,
		SaveSession:      root.State().SaveSession,
		ClearThreadContext: func(sess *state.Session) {
			clearSessionThreadContext(sess)
		},
		SetThreadContext: func(sess *state.Session, workspaceID, threadID, name, preview string) {
			setSessionThreadContext(sess, workspaceID, threadID, name, preview)
		},
		ResetActiveOps:     sessionResetActiveOperations,
		MarkThreadLive:     func(sessionKey, threadID string) { markSessionThreadLive(root, sessionKey, threadID) },
		DefaultWorkspaceID: func() string { return defaultWorkspaceID(root) },
		ResolveModel: func(sess *state.Session, ws *config.Workspace) string {
			return firstNonEmpty(strings.TrimSpace(sess.ModelOverride), strings.TrimSpace(ws.Model), strings.TrimSpace(root.cfg.Claude.Model))
		},
	}, sessionKey, sess, ws, sel)
}

func (a convBackendConversationAdapter) InterruptClaudeTurn(app appconvbackend.App, ctx context.Context, sessionKey string) error {
	root := app.(*App)
	if root == nil || root.claude == nil {
		return fmt.Errorf("claude backend not initialized")
	}
	return root.claude.Interrupt(ctx, sessionKey)
}

func (a convBackendConversationAdapter) ContinueClaudeTurn(app appconvbackend.App, sessionKey, text string) error {
	return newReplyContinuationService(app.(*App)).continueClaudeSessionWithText(sessionKey, text)
}

func (a convBackendConversationAdapter) TryClaudeReplyContinuation(app appconvbackend.App, msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	return newReplyContinuationService(app.(*App)).tryClaudeReplyContinuation(msg, link, sessionKey, sess)
}

func (a convBackendConversationAdapter) ForkClaudeConversation(app appconvbackend.App, sessionKey string, sess *state.Session, ws *config.Workspace) (string, error) {
	return forkClaudeActiveConversation(app.(*App), sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) RecoverClaudeStartup(app appconvbackend.App, sessionKey, workspaceID string, sess *state.Session) {
	appconvbackend.RecoverClaudeStartupConversation(appconvbackend.ClaudeStartupRecoveryDeps{
		MarkThreadLive: func(sessionKey, threadID string) { markSessionThreadLive(app.(*App), sessionKey, threadID) },
	}, sessionKey, workspaceID, sess)
}

func (a convBackendConversationAdapter) StartNextSubmission(app appconvbackend.App, sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	if appcore.ConfiguredBackend(app) == "claude" {
		return newSubmissionQueueServiceFromApp(app.(*App)).StartNextClaudeSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
	}
	return newSubmissionQueueServiceFromApp(app.(*App)).StartNextCodexSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
}

func (a convBackendConversationAdapter) MarkThreadLive(app appconvbackend.App, sessionKey, threadID string) {
	markSessionThreadLive(app.(*App), sessionKey, threadID)
}

// ---------------------------------------------------------------------------
// WorkspaceConfigProvider adapter
// ---------------------------------------------------------------------------

type convBackendWorkspaceConfigAdapter struct{}

func (a convBackendWorkspaceConfigAdapter) HistoryIndexForOrdinal(app appconvbackend.App, sessionKey string, ordinal int) (int, error) {
	return apphistorycmd.NewService(app.(*App)).CodexHistoryIndexForOrdinal(sessionKey, ordinal)
}

func (a convBackendWorkspaceConfigAdapter) RenderCodexHistoryCard(app appconvbackend.App, sessionKey string, page int) (map[string]any, error) {
	return apphistorycmd.NewService(app.(*App)).RenderCodexHistoryCard(sessionKey, page)
}

func (a convBackendWorkspaceConfigAdapter) RenderCodexHistoryDetailCard(app appconvbackend.App, sessionKey string, index int) (map[string]any, error) {
	return apphistorycmd.NewService(app.(*App)).RenderCodexHistoryDetailCard(sessionKey, index)
}

func (a convBackendWorkspaceConfigAdapter) RenderCodexUsageBody(app appconvbackend.App, sess *state.Session) string {
	return appdebugviewcmd.NewUsageService(app.(*App)).RenderCodexUsageBody(sess)
}

func (a convBackendWorkspaceConfigAdapter) HistoryTurnIndexForOrdinal(app appconvbackend.App, sessionKey string, ordinal int) (int, error) {
	return historyTurnIndexForOrdinal(app.(*App), sessionKey, ordinal)
}

func (a convBackendWorkspaceConfigAdapter) RenderClaudeHistoryCard(app appconvbackend.App, sessionKey string, page int) (map[string]any, error) {
	return renderClaudeHistoryCard(app.(*App), sessionKey, page)
}

func (a convBackendWorkspaceConfigAdapter) RenderClaudeHistoryDetailCard(app appconvbackend.App, sessionKey string, index int) (map[string]any, error) {
	return renderClaudeHistoryDetailCard(app.(*App), sessionKey, index)
}

func (a convBackendWorkspaceConfigAdapter) RenderClaudeUsageBody(app appconvbackend.App, sess *state.Session) string {
	return appdebugviewcmd.NewUsageService(app.(*App)).RenderClaudeUsageBody(sess)
}

// ---------------------------------------------------------------------------
// *App methods satisfying convbackend.App
// ---------------------------------------------------------------------------

func (a *App) ConvBackendState() appconvbackend.AppStateProvider {
	if a == nil {
		return nil
	}
	return a.State()
}

func (a *App) ConvBackendConversation() appconvbackend.ConversationProvider {
	return convBackendConversationAdapter{}
}

func (a *App) ConvBackendWorkspaceConfig() appconvbackend.WorkspaceConfigProvider {
	return convBackendWorkspaceConfigAdapter{}
}
