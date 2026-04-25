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
	startQueuedSubmission(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error
}

func conversationBackend(a *App) conversationBackendFacade {
	if runtime := backendRuntime(a); runtime != nil {
		return runtime.conversationBackend(a)
	}
	return codexConversationBackend{deps: a}
}

type codexConversationBackend struct {
	deps BackendDeps
}

func (b codexConversationBackend) listWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return newWorkspaceThreadService(b.deps.App()).listCodexWorkspaceThreads(sessionKey, ws, includeAll)
}

func (b codexConversationBackend) ensureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return newWorkspaceThreadService(b.deps.App()).ensureCodexWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (b codexConversationBackend) startWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return newWorkspaceThreadService(b.deps.App()).startCodexWorkspaceThread(sessionKey, sess, ws)
}

func (b codexConversationBackend) resumeSelectedThread(sessionKey string, sess *state.Session, ws *config.Workspace, selection threadResumeSelection) (*workspaceThreadBinding, error) {
	return resumeCodexSelectedThread(b.deps.App(), sessionKey, sess, ws, selection)
}

func (b codexConversationBackend) forkActiveConversation(sessionKey string, sess *state.Session, ws *config.Workspace) (string, error) {
	return forkCodexActiveConversation(b.deps.App(), sessionKey, sess, ws)
}

func (b codexConversationBackend) forkReplyMessage(string) string {
	return "已 fork 当前线程，并切换到新的分支线程。"
}

func (b codexConversationBackend) recoverStartupConversation(sessionKey, workspaceID string, sess *state.Session, ws *config.Workspace, effectiveModel string) {
	recoverCodexStartupConversation(b.deps.App(), sessionKey, workspaceID, sess, ws, effectiveModel)
}

func (b codexConversationBackend) renderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	return renderCodexThreadsCard(b.deps.App(), sessionKey, includeAll)
}

func (b codexConversationBackend) historyIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	return newHistoryService(b.deps.App()).codexHistoryIndexForOrdinal(sessionKey, ordinal)
}

func (b codexConversationBackend) renderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	return newHistoryService(b.deps.App()).renderCodexHistoryCard(sessionKey, page)
}

func (b codexConversationBackend) renderHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	return newHistoryService(b.deps.App()).renderCodexHistoryDetailCard(sessionKey, index)
}

func (b codexConversationBackend) renderUsageBody(sess *state.Session) string {
	return newUsageService(b.deps.App()).renderCodexUsageBody(sess)
}

func (b codexConversationBackend) interruptActiveTurn(ctx context.Context, _ string, sess *state.Session) error {
	return interruptCodexActiveTurn(b.deps.App(), ctx, sess)
}

func (b codexConversationBackend) continueActiveTurn(sessionKey, text string) error {
	return continueCodexActiveTurn(b.deps.App(), sessionKey, text)
}

func (b codexConversationBackend) tryReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	return tryCodexReplyContinuation(b.deps.App(), msg, link, sessionKey, sess)
}

func (b codexConversationBackend) startQueuedSubmission(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	return newSubmissionCoordinator(b.deps.App()).startNextCodexSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
}

type claudeConversationBackend struct {
	deps BackendDeps
}

func (b claudeConversationBackend) listWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return listClaudeSessions(sessionKey, ws, includeAll)
}

func (b claudeConversationBackend) ensureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return newWorkspaceThreadService(b.deps.App()).ensureClaudeWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (b claudeConversationBackend) startWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return newWorkspaceThreadService(b.deps.App()).startClaudeWorkspaceThread(sessionKey, sess, ws)
}

func (b claudeConversationBackend) resumeSelectedThread(sessionKey string, sess *state.Session, ws *config.Workspace, selection threadResumeSelection) (*workspaceThreadBinding, error) {
	return resumeClaudeSelectedThread(b.deps.App(), sessionKey, sess, ws, selection)
}

func (b claudeConversationBackend) forkActiveConversation(sessionKey string, sess *state.Session, ws *config.Workspace) (string, error) {
	return forkClaudeActiveConversation(b.deps.App(), sessionKey, sess, ws)
}

func (b claudeConversationBackend) forkReplyMessage(forkedID string) string {
	if strings.TrimSpace(forkedID) == "" {
		return "已准备 fork 当前会话。新的 Claude 分支会话会在下一条消息时创建并切换。"
	}
	return "已 fork 当前会话，并切换到新的分支会话。"
}

func (b claudeConversationBackend) recoverStartupConversation(sessionKey, workspaceID string, sess *state.Session, _ *config.Workspace, _ string) {
	recoverClaudeStartupConversation(b.deps.App(), sessionKey, workspaceID, sess)
}

func (b claudeConversationBackend) renderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	return renderClaudeThreadsCardForCurrentBackend(b.deps.App(), sessionKey, includeAll)
}

func (b claudeConversationBackend) historyIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	return historyTurnIndexForOrdinal(b.deps.App(), sessionKey, ordinal)
}

func (b claudeConversationBackend) renderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	return renderClaudeHistoryCard(b.deps.App(), sessionKey, page)
}

func (b claudeConversationBackend) renderHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	return renderClaudeHistoryDetailCard(b.deps.App(), sessionKey, index)
}

func (b claudeConversationBackend) renderUsageBody(sess *state.Session) string {
	return newUsageService(b.deps.App()).renderClaudeUsageBody(sess)
}

func (b claudeConversationBackend) interruptActiveTurn(ctx context.Context, sessionKey string, _ *state.Session) error {
	return interruptClaudeActiveTurn(b.deps.App(), ctx, sessionKey)
}

func (b claudeConversationBackend) continueActiveTurn(sessionKey, text string) error {
	return newReplyContinuationService(b.deps.App()).continueClaudeSessionWithText(sessionKey, text)
}

func (b claudeConversationBackend) tryReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	return newReplyContinuationService(b.deps.App()).tryClaudeReplyContinuation(msg, link, sessionKey, sess)
}

func (b claudeConversationBackend) startQueuedSubmission(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	return newClaudeSubmissionService(b.deps.App()).startNextClaudeSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
}
