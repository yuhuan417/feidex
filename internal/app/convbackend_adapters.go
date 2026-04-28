package app

import (
	"context"

	appcore "feidex/internal/app/appcore"
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
	return newWorkspaceThreadServiceInner(app.(*App)).ListCodexWorkspaceThreads(sessionKey, ws, includeAll)
}

func (a convBackendConversationAdapter) EnsureCodexBinding(app appconvbackend.App, sessionKey string, sess *state.Session, ws *config.Workspace) (*appconvbackend.ThreadBinding, error) {
	return newWorkspaceThreadServiceInner(app.(*App)).EnsureCodexWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) StartCodexThread(app appconvbackend.App, sessionKey string, sess *state.Session, ws *config.Workspace) (*appconvbackend.ThreadBinding, error) {
	return newWorkspaceThreadServiceInner(app.(*App)).StartCodexWorkspaceThread(sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) ResumeCodexThread(app appconvbackend.App, sessionKey string, sess *state.Session, ws *config.Workspace, sel appconvbackend.ThreadResumeSelection) (*appconvbackend.ThreadBinding, error) {
	return resumeCodexSelectedThread(app.(*App), sessionKey, sess, ws, threadResumeSelection{
		ThreadID: sel.ThreadID,
		Name:     sel.Name,
		Preview:  sel.Preview,
		Cwd:      sel.Cwd,
	})
}

func (a convBackendConversationAdapter) InterruptCodexTurn(app appconvbackend.App, ctx context.Context, sess *state.Session) error {
	return interruptCodexActiveTurn(app.(*App), ctx, sess)
}

func (a convBackendConversationAdapter) ContinueCodexTurn(app appconvbackend.App, sessionKey, text string) error {
	return continueCodexActiveTurn(app.(*App), sessionKey, text)
}

func (a convBackendConversationAdapter) TryCodexReplyContinuation(app appconvbackend.App, msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	return tryCodexReplyContinuation(app.(*App), msg, link, sessionKey, sess)
}

func (a convBackendConversationAdapter) ForkCodexConversation(app appconvbackend.App, sessionKey string, sess *state.Session, ws *config.Workspace) (string, error) {
	return forkCodexActiveConversation(app.(*App), sessionKey, sess, ws)
}

func (a convBackendConversationAdapter) RecoverCodexStartup(app appconvbackend.App, sessionKey, workspaceID string, sess *state.Session, ws *config.Workspace, effectiveModel string) {
	recoverCodexStartupConversation(app.(*App), sessionKey, workspaceID, sess, ws, effectiveModel)
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
	return resumeClaudeSelectedThread(app.(*App), sessionKey, sess, ws, threadResumeSelection{
		ThreadID: sel.ThreadID,
		Name:     sel.Name,
		Preview:  sel.Preview,
		Cwd:      sel.Cwd,
	})
}

func (a convBackendConversationAdapter) InterruptClaudeTurn(app appconvbackend.App, ctx context.Context, sessionKey string) error {
	return interruptClaudeActiveTurn(app.(*App), ctx, sessionKey)
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
	recoverClaudeStartupConversation(app.(*App), sessionKey, workspaceID, sess)
}

func (a convBackendConversationAdapter) StartNextSubmission(app appconvbackend.App, sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	if appcore.ConfiguredBackend(app) == "claude" {
		return newClaudeSubmissionService(app.(*App)).startNextClaudeSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
	}
	return newSubmissionCoordinator(app.(*App)).startNextCodexSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
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

func (a convBackendWorkspaceConfigAdapter) RenderCodexUsageBody(app appconvbackend.App, sess *state.Session) string {
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

func (a convBackendWorkspaceConfigAdapter) RenderClaudeUsageBody(app appconvbackend.App, sess *state.Session) string {
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
