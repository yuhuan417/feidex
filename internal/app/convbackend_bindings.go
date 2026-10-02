package app

import (
	"context"
	"feidex/internal/domain/conversation"
	"fmt"

	appcore "feidex/internal/app/appcore"
	"feidex/internal/app/attachments"
	"feidex/internal/app/claudesession"
	appconvbackend "feidex/internal/app/convbackend"
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
	return newWorkspaceThreadService(app.(*App)).ListCodexWorkspaceThreads(sessionKey, ws, includeAll)
}

func (a convBackendConversationAdapter) EnsureCodexBinding(app appconvbackend.App, sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appconvbackend.ThreadBinding, error) {
	return newWorkspaceThreadService(app.(*App)).EnsureCodexWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) StartCodexThread(app appconvbackend.App, sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appconvbackend.ThreadBinding, error) {
	return newWorkspaceThreadService(app.(*App)).StartCodexWorkspaceThread(sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) ResumeCodexThread(app appconvbackend.App, sessionKey string, sess *conversation.Session, ws *config.Workspace, sel appconvbackend.ThreadResumeSelection) (*appconvbackend.ThreadBinding, error) {
	root := app.(*App)
	return appconvbackend.ResumeCodexSelectedThread(appconvbackend.CodexResumeDeps{
		Context:           root.Context,
		RequireClient:     func() (appconvbackend.CodexRPCClient, error) { return requireCodexClient(root) },
		SaveSession:       root.State().SaveSession,
		BuildThreadConfig: func(sess *conversation.Session) map[string]any { return codexAuxiliaryConfig(root, sess) },
		SetThreadContext: func(sess *conversation.Session, workspaceID, threadID, name, preview string) {
			conversation.SetThreadContext(sess, workspaceID, threadID, name, preview)
		},
		ResetActiveOps:     conversation.ResetActiveOperations,
		MarkThreadLive:     func(sessionKey, threadID string) { markSessionThreadLive(root, sessionKey, threadID) },
		DefaultWorkspaceID: func() string { return defaultWorkspaceID(root) },
		ConfiguredModel:    func() string { return effectiveCodexModel(root, sess, ws) },
	}, sessionKey, sess, ws, sel)
}

func (a convBackendConversationAdapter) InterruptCodexTurn(app appconvbackend.App, ctx context.Context, sess *conversation.Session) error {
	root := app.(*App)
	err := appconvbackend.InterruptCodexActiveTurn(appconvbackend.CodexInterruptDeps{
		Context:       root.Context,
		RequireClient: func() (appconvbackend.CodexRPCClient, error) { return requireCodexClient(root) },
	}, ctx, sess)
	if err != nil && sess != nil {
		// The turn may have failed just before interrupt reached the server. Only
		// authoritative terminal history (or an arrived completion) can close it.
		updated := reconcileCompletedCodexTurn(root, sess.Key, sess)
		if updated == nil || updated.ActiveTurnID != sess.ActiveTurnID {
			return nil
		}
	}
	return err
}

func (a convBackendConversationAdapter) ContinueCodexTurn(app appconvbackend.App, sessionKey, text string) error {
	root := app.(*App)
	return appconvbackend.ContinueCodexActiveTurn(appconvbackend.CodexContinueDeps{
		Context:       root.Context,
		RequireClient: func() (appconvbackend.CodexRPCClient, error) { return requireCodexClient(root) },
		GetSession:    root.State().Session,
	}, sessionKey, text)
}

func (a convBackendConversationAdapter) TryCodexReplyContinuation(app appconvbackend.App, msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *conversation.Session) (bool, error) {
	root := app.(*App)
	replySvc := newReplyContinuationService(root)
	return appconvbackend.TryCodexReplyContinuation(appconvbackend.CodexReplyContinuationDeps{
		Context:       root.Context,
		RequireClient: func() (appconvbackend.CodexRPCClient, error) { return requireCodexClient(root) },
		ResolveInboundAttachments: func(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]state.SubmissionAttachment, error) {
			return resolveInboundAttachments(root, msg, workspaceID, sessionKey)
		},
		PendingInputSessionKey:     replySvc.pendingInputSessionKey,
		CollectPendingStagedImages: replySvc.collectPendingStagedImages,
		ClearPendingStagedImages:   replySvc.clearPendingStagedImages,
		BuildTurnInputs:            attachments.BuildTurnInputs,
		SaveSession:                root.State().SaveSession,
		DefaultWorkspaceID:         func() string { return defaultWorkspaceID(root) },
	}, msg, link, sessionKey, sess)
}

func (a convBackendConversationAdapter) ForkCodexConversation(app appconvbackend.App, sessionKey string, sess *conversation.Session, ws *config.Workspace) (string, error) {
	return forkCodexActiveConversation(app.(*App), sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) RecoverCodexStartup(app appconvbackend.App, sessionKey, workspaceID string, sess *conversation.Session, ws *config.Workspace, effectiveModel string) {
	root := app.(*App)
	appconvbackend.RecoverCodexStartupConversation(appconvbackend.CodexStartupRecoveryDeps{
		Context:       root.Context,
		CurrentClient: func() appconvbackend.CodexRPCClient { return currentCodexClient(root) },
		RuntimeRecovering: func() bool {
			return codexRuntimeRecovering(root)
		},
		BuildThreadStartParams: func(ws *config.Workspace, sess *conversation.Session, effectiveModel string) codexrpc.ThreadStartParams {
			return buildThreadStartParams(root, ws, sess, effectiveModel)
		},
		BuildThreadConfig: func(sess *conversation.Session) map[string]any { return codexAuxiliaryConfig(root, sess) },
		SaveSession:       root.State().SaveSession,
		SetThreadContext: func(sess *conversation.Session, workspaceID, threadID, name, preview string) {
			conversation.SetThreadContext(sess, workspaceID, threadID, name, preview)
		},
		ClearThreadContext:     conversation.ClearThreadContext,
		MarkThreadLive:         func(sessionKey, threadID string) { markSessionThreadLive(root, sessionKey, threadID) },
		ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(root, sessionKey) },
	}, sessionKey, workspaceID, sess, ws, effectiveModel)
}

func (a convBackendConversationAdapter) ListClaudeThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return claudesession.ListSessions(sessionKey, ws, includeAll)
}

func (a convBackendConversationAdapter) EnsureClaudeBinding(app appconvbackend.App, sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appconvbackend.ThreadBinding, error) {
	return newWorkspaceThreadService(app.(*App)).EnsureClaudeWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) StartClaudeThread(app appconvbackend.App, sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appconvbackend.ThreadBinding, error) {
	return newWorkspaceThreadService(app.(*App)).StartClaudeWorkspaceThread(sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) ResumeClaudeThread(app appconvbackend.App, sessionKey string, sess *conversation.Session, ws *config.Workspace, sel appconvbackend.ThreadResumeSelection) (*appconvbackend.ThreadBinding, error) {
	root := app.(*App)
	return appconvbackend.ResumeClaudeSelectedThread(appconvbackend.ClaudeResumeDeps{
		Context:          root.Context,
		FindSessionEntry: claudesession.FindSessionEntry,
		EnsureSession:    root.claude,
		SaveSession:      root.State().SaveSession,
		ClearThreadContext: func(sess *conversation.Session) {
			conversation.ClearThreadContext(sess)
		},
		SetThreadContext: func(sess *conversation.Session, workspaceID, threadID, name, preview string) {
			conversation.SetThreadContext(sess, workspaceID, threadID, name, preview)
		},
		ResetActiveOps:     conversation.ResetActiveOperations,
		MarkThreadLive:     func(sessionKey, threadID string) { markSessionThreadLive(root, sessionKey, threadID) },
		DefaultWorkspaceID: func() string { return defaultWorkspaceID(root) },
		ResolveModel: func(sess *conversation.Session, ws *config.Workspace) string {
			return effectiveClaudeModel(root, sess, ws)
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

func (a convBackendConversationAdapter) TryClaudeReplyContinuation(app appconvbackend.App, msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *conversation.Session) (bool, error) {
	return newReplyContinuationService(app.(*App)).tryClaudeReplyContinuation(msg, link, sessionKey, sess)
}

func (a convBackendConversationAdapter) ForkClaudeConversation(app appconvbackend.App, sessionKey string, sess *conversation.Session, ws *config.Workspace) (string, error) {
	return forkClaudeActiveConversation(app.(*App), sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) RecoverClaudeStartup(app appconvbackend.App, sessionKey, workspaceID string, sess *conversation.Session) {
	appconvbackend.RecoverClaudeStartupConversation(appconvbackend.ClaudeStartupRecoveryDeps{
		Context:        app.(*App).Context,
		MarkThreadLive: func(sessionKey, threadID string) { markSessionThreadLive(app.(*App), sessionKey, threadID) },
	}, sessionKey, workspaceID, sess)
}

func (a convBackendConversationAdapter) StartNextSubmission(app appconvbackend.App, sessionKey string, sess *conversation.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
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
	return newHistoryService(app.(*App)).CodexHistoryIndexForOrdinal(sessionKey, ordinal)
}

func (a convBackendWorkspaceConfigAdapter) RenderCodexHistoryCard(app appconvbackend.App, sessionKey string, page int) (map[string]any, error) {
	return newHistoryService(app.(*App)).RenderCodexHistoryCard(sessionKey, page)
}

func (a convBackendWorkspaceConfigAdapter) RenderCodexHistoryDetailCard(app appconvbackend.App, sessionKey string, index int) (map[string]any, error) {
	return newHistoryService(app.(*App)).RenderCodexHistoryDetailCard(sessionKey, index)
}

func (a convBackendWorkspaceConfigAdapter) RenderCodexUsageBody(app appconvbackend.App, sess *conversation.Session) string {
	return newUsageService(app.(*App)).RenderCodexUsageBody(sess)
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

func (a convBackendWorkspaceConfigAdapter) RenderClaudeUsageBody(app appconvbackend.App, sess *conversation.Session) string {
	return newUsageService(app.(*App)).RenderClaudeUsageBody(sess)
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
