package app

import (
	"context"

	appconvbackend "feidex/internal/app/convbackend"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// threadResumeSelection is a thin alias for the sub-package type.
type threadResumeSelection = appconvbackend.ThreadResumeSelection

// conversationBackendFacade is the local interface that mirrors the
// sub-package's exported ConversationBackendFacade with lowercase method
// names. This preserves backward compatibility with existing callers.
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

// conversationBackendWrapper adapts the sub-package's exported
// ConversationBackendFacade to the local lowercase conversationBackendFacade
// interface, preserving backward compatibility with existing callers.
type conversationBackendWrapper struct {
	inner appconvbackend.ConversationBackendFacade
}

func (w conversationBackendWrapper) listWorkspaceThreads(sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return w.inner.ListWorkspaceThreads(sessionKey, ws, includeAll)
}

func (w conversationBackendWrapper) ensureWorkspaceThreadBinding(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return w.inner.EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (w conversationBackendWrapper) startWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*workspaceThreadBinding, error) {
	return w.inner.StartWorkspaceThread(sessionKey, sess, ws)
}

func (w conversationBackendWrapper) resumeSelectedThread(sessionKey string, sess *state.Session, ws *config.Workspace, selection threadResumeSelection) (*workspaceThreadBinding, error) {
	return w.inner.ResumeSelectedThread(sessionKey, sess, ws, appconvbackend.ThreadResumeSelection{
		ThreadID: selection.ThreadID,
		Name:     selection.Name,
		Preview:  selection.Preview,
		Cwd:      selection.Cwd,
	})
}

func (w conversationBackendWrapper) forkActiveConversation(sessionKey string, sess *state.Session, ws *config.Workspace) (string, error) {
	return w.inner.ForkActiveConversation(sessionKey, sess, ws)
}

func (w conversationBackendWrapper) forkReplyMessage(forkedID string) string {
	return w.inner.ForkReplyMessage(forkedID)
}

func (w conversationBackendWrapper) recoverStartupConversation(sessionKey, workspaceID string, sess *state.Session, ws *config.Workspace, effectiveModel string) {
	w.inner.RecoverStartupConversation(sessionKey, workspaceID, sess, ws, effectiveModel)
}

func (w conversationBackendWrapper) renderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	return w.inner.RenderThreadsCard(sessionKey, includeAll)
}

func (w conversationBackendWrapper) historyIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	return w.inner.HistoryIndexForOrdinal(sessionKey, ordinal)
}

func (w conversationBackendWrapper) renderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	return w.inner.RenderHistoryCard(sessionKey, page)
}

func (w conversationBackendWrapper) renderHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	return w.inner.RenderHistoryDetailCard(sessionKey, index)
}

func (w conversationBackendWrapper) renderUsageBody(sess *state.Session) string {
	return w.inner.RenderUsageBody(sess)
}

func (w conversationBackendWrapper) interruptActiveTurn(ctx context.Context, sessionKey string, sess *state.Session) error {
	return w.inner.InterruptActiveTurn(ctx, sessionKey, sess)
}

func (w conversationBackendWrapper) continueActiveTurn(sessionKey, text string) error {
	return w.inner.ContinueActiveTurn(sessionKey, text)
}

func (w conversationBackendWrapper) tryReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	return w.inner.TryReplyContinuation(msg, link, sessionKey, sess)
}

func (w conversationBackendWrapper) startQueuedSubmission(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	return w.inner.StartQueuedSubmission(sessionKey, sess, sub, ws, notifyFailure)
}

// conversationBackend creates the appropriate conversation backend facade
// for the current backend, wrapping the sub-package's exported type.
func conversationBackend(a *App) conversationBackendFacade {
	if runtime := backendRuntime(a); runtime != nil {
		return conversationBackendWrapper{inner: runtime.conversationBackend(a)}
	}
	return conversationBackendWrapper{inner: appconvbackend.NewCodexConversationBackend(a)}
}
