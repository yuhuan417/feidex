package app

import (
	appcompact "feidex/internal/app/compact"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// Type and constant aliases — compact sub-package
// ---------------------------------------------------------------------------

type compactService = appcompact.Service

var newCompactService = appcompact.NewService

const sessionStatusCompacting = appcompact.SessionStatusCompacting

// ---------------------------------------------------------------------------
// Thin wrappers — canonical logic lives in compact.Service
// ---------------------------------------------------------------------------

func isContextCompactionItem(item map[string]any) bool {
	return appcompact.IsContextCompactionItem(item)
}

func sessionHasActiveWork(sess *state.Session) bool {
	return appcompact.SessionHasActiveWork(sess)
}

func commandCompact(a *App, msg *feishu.InboundMessage, args []string) error {
	return newCompactService(a).CommandCompact(msg, args)
}

func compactMenuButtons(sessionKey string, includeRetry bool) []feishu.Button {
	return appcompact.CompactMenuButtons(sessionKey, includeRetry)
}

func renderCompactPreparingCard(a *App, sessionKey string) map[string]any {
	return newCompactService(a).RenderCompactPreparingCard(sessionKey)
}

func renderCompactAcceptedCard(a *App, sessionKey string) map[string]any {
	return newCompactService(a).RenderCompactAcceptedCard(sessionKey)
}

func renderCompactFailedCard(a *App, sessionKey, errText string) map[string]any {
	return newCompactService(a).RenderCompactFailedCard(sessionKey, errText)
}

func runMenuCompactAction(a *App, action *feishu.CardAction, sessionKey string) error {
	return newCompactService(a).RunMenuCompactAction(sessionKey, action)
}

func startThreadCompaction(a *App, sessionKey string) (*state.Session, error) {
	return newCompactService(a).StartThreadCompaction(sessionKey)
}

func bindStandaloneCompactTurn(a *App, threadID, turnID string) bool {
	return newCompactService(a).BindStandaloneCompactTurn(threadID, turnID)
}

func noteStandaloneCompactItemStarted(a *App, threadID, turnID string, item map[string]any) bool {
	return newCompactService(a).NoteStandaloneCompactItemStarted(threadID, turnID, item)
}

func completeStandaloneCompactTurn(a *App, threadID, turnID string) bool {
	return newCompactService(a).CompleteStandaloneCompactTurn(threadID, turnID)
}

func completeStandaloneCompactItem(a *App, threadID, turnID string, item map[string]any) bool {
	return newCompactService(a).CompleteStandaloneCompactItem(threadID, turnID, item)
}

func finishStandaloneCompactTurn(a *App, threadID, turnID, status string) bool {
	return newCompactService(a).FinishStandaloneCompactTurn(threadID, turnID, status)
}

func failStandaloneCompactTurn(a *App, threadID, turnID, message string) bool {
	return newCompactService(a).FailStandaloneCompactTurn(threadID, turnID, message)
}

func restoreStandaloneCompactSession(a *App, sessionKey, threadID, previousStatus string) {
	newCompactService(a).RestoreStandaloneCompactSession(sessionKey, threadID, previousStatus)
}

func sendStandaloneCompactResult(a *App, sess *state.Session, status string) {
	text := appcompact.StandaloneCompactResultText(status)
	if text == "" {
		return
	}
	newCompactService(a).SendSessionTextNotice(sess, text)
}

func sendSessionTextNotice(a *App, sess *state.Session, text string) {
	newCompactService(a).SendSessionTextNotice(sess, text)
}

func standaloneCompactResultText(status string) string {
	return appcompact.StandaloneCompactResultText(status)
}
