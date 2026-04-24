package app

import (
	"context"
	"strings"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type threadResumeSelection struct {
	ThreadID string
	Name     string
	Preview  string
	Cwd      string
}

type conversationBackendFacade interface {
	listWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error)
	ensureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error)
	startWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error)
	resumeSelectedThread(sessionKey string, sess *state.Session, ws *config.Workspace, selection threadResumeSelection) (*workspaceThreadBinding, error)
	forkActiveConversation(sessionKey string, sess *state.Session, ws *config.Workspace) (string, error)
	forkReplyMessage(forkedID string) string
	recoverStartupConversation(sessionKey, workspaceID string, sess *state.Session, ws *config.Workspace, effectiveModel string)
	renderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error)
	historyIndexForOrdinal(sessionKey string, ordinal int) (int, error)
	renderHistoryCard(sessionKey string, page int) (map[string]any, error)
	renderHistoryDetailCard(sessionKey string, index int) (map[string]any, error)
	renderUsageBody(sess *state.Session) string
	interruptActiveTurn(ctx context.Context, sessionKey string, sess *state.Session) error
	continueActiveTurn(sessionKey, text string) error
	tryReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error)
	startQueuedSubmission(w *lifecycleCoordinator, sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error
}

func conversationBackend(a *App) conversationBackendFacade {
	if runtime := backendRuntime(a); runtime != nil {
		return runtime.conversationBackend(a)
	}
	return codexConversationBackend{app: a}
}

type codexConversationBackend struct {
	app *App
}

func (b codexConversationBackend) listWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return newWorkspaceThreadService(b.app).listCodexWorkspaceThreads(sessionKey, ws, includeAll)
}

func (b codexConversationBackend) ensureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return newWorkspaceThreadService(b.app).ensureCodexWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (b codexConversationBackend) startWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return newWorkspaceThreadService(b.app).startCodexWorkspaceThread(sessionKey, sess, ws)
}

func (b codexConversationBackend) resumeSelectedThread(sessionKey string, sess *state.Session, ws *config.Workspace, selection threadResumeSelection) (*workspaceThreadBinding, error) {
	return resumeCodexSelectedThread(b.app, sessionKey, sess, ws, selection)
}

func (b codexConversationBackend) forkActiveConversation(sessionKey string, sess *state.Session, ws *config.Workspace) (string, error) {
	return forkCodexActiveConversation(b.app, sessionKey, sess, ws)
}

func (b codexConversationBackend) forkReplyMessage(string) string {
	return "已 fork 当前线程，并切换到新的分支线程。"
}

func (b codexConversationBackend) recoverStartupConversation(sessionKey, workspaceID string, sess *state.Session, ws *config.Workspace, effectiveModel string) {
	recoverCodexStartupConversation(b.app, sessionKey, workspaceID, sess, ws, effectiveModel)
}

func (b codexConversationBackend) renderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	return renderCodexThreadsCard(b.app, sessionKey, includeAll)
}

func (b codexConversationBackend) historyIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	return newHistoryService(b.app).codexHistoryIndexForOrdinal(sessionKey, ordinal)
}

func (b codexConversationBackend) renderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	return newHistoryService(b.app).renderCodexHistoryCard(sessionKey, page)
}

func (b codexConversationBackend) renderHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	return newHistoryService(b.app).renderCodexHistoryDetailCard(sessionKey, index)
}

func (b codexConversationBackend) renderUsageBody(sess *state.Session) string {
	return newUsageService(b.app).renderCodexUsageBody(sess)
}

func (b codexConversationBackend) interruptActiveTurn(ctx context.Context, _ string, sess *state.Session) error {
	return interruptCodexActiveTurn(b.app, ctx, sess)
}

func (b codexConversationBackend) continueActiveTurn(sessionKey, text string) error {
	return continueCodexActiveTurn(b.app, sessionKey, text)
}

func (b codexConversationBackend) tryReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	return tryCodexReplyContinuation(b.app, msg, link, sessionKey, sess)
}

func (b codexConversationBackend) startQueuedSubmission(w *lifecycleCoordinator, sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	return w.startNextCodexSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
}

type claudeConversationBackend struct {
	app *App
}

func (b claudeConversationBackend) listWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return listClaudeSessions(sessionKey, ws, includeAll)
}

func (b claudeConversationBackend) ensureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return newWorkspaceThreadService(b.app).ensureClaudeWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (b claudeConversationBackend) startWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return newWorkspaceThreadService(b.app).startClaudeWorkspaceThread(sessionKey, sess, ws)
}

func (b claudeConversationBackend) resumeSelectedThread(sessionKey string, sess *state.Session, ws *config.Workspace, selection threadResumeSelection) (*workspaceThreadBinding, error) {
	return resumeClaudeSelectedThread(b.app, sessionKey, sess, ws, selection)
}

func (b claudeConversationBackend) forkActiveConversation(sessionKey string, sess *state.Session, ws *config.Workspace) (string, error) {
	return forkClaudeActiveConversation(b.app, sessionKey, sess, ws)
}

func (b claudeConversationBackend) forkReplyMessage(forkedID string) string {
	if strings.TrimSpace(forkedID) == "" {
		return "已准备 fork 当前会话。新的 Claude 分支会话会在下一条消息时创建并切换。"
	}
	return "已 fork 当前会话，并切换到新的分支会话。"
}

func (b claudeConversationBackend) recoverStartupConversation(sessionKey, workspaceID string, sess *state.Session, _ *config.Workspace, _ string) {
	recoverClaudeStartupConversation(b.app, sessionKey, workspaceID, sess)
}

func (b claudeConversationBackend) renderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	return renderClaudeThreadsCardForCurrentBackend(b.app, sessionKey, includeAll)
}

func (b claudeConversationBackend) historyIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	return historyTurnIndexForOrdinal(b.app, sessionKey, ordinal)
}

func (b claudeConversationBackend) renderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	return renderClaudeHistoryCard(b.app, sessionKey, page)
}

func (b claudeConversationBackend) renderHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	return renderClaudeHistoryDetailCard(b.app, sessionKey, index)
}

func (b claudeConversationBackend) renderUsageBody(sess *state.Session) string {
	return newUsageService(b.app).renderClaudeUsageBody(sess)
}

func (b claudeConversationBackend) interruptActiveTurn(ctx context.Context, sessionKey string, _ *state.Session) error {
	return interruptClaudeActiveTurn(b.app, ctx, sessionKey)
}

func (b claudeConversationBackend) continueActiveTurn(sessionKey, text string) error {
	return newReplyContinuationService(b.app).continueClaudeSessionWithText(sessionKey, text)
}

func (b claudeConversationBackend) tryReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	return newReplyContinuationService(b.app).tryClaudeReplyContinuation(msg, link, sessionKey, sess)
}

func (b claudeConversationBackend) startQueuedSubmission(w *lifecycleCoordinator, sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	return w.startNextClaudeSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
}
