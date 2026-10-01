package app

import (
	appcompact "feidex/internal/app/compact"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const sessionStatusCompacting = "compacting"

// ---------------------------------------------------------------------------
// Thin wrappers — canonical logic lives in compact.Service
// ---------------------------------------------------------------------------

func sessionHasActiveWork(sess *state.Session) bool {
	return appcompact.SessionHasActiveWork(sess)
}

func commandCompact(a *App, msg *feishu.InboundMessage, args []string) error {
	return appcompact.NewService(a).CommandCompact(msg, args)
}

func renderCompactPreparingCard(a *App, sessionKey string) map[string]any {
	return appcompact.NewService(a).RenderCompactPreparingCard(sessionKey)
}

func renderCompactAcceptedCard(a *App, sessionKey string) map[string]any {
	return appcompact.NewService(a).RenderCompactAcceptedCard(sessionKey)
}

func renderCompactFailedCard(a *App, sessionKey, errText string) map[string]any {
	return appcompact.NewService(a).RenderCompactFailedCard(sessionKey, errText)
}

func runMenuCompactAction(a *App, action *feishu.CardAction, sessionKey string) error {
	return appcompact.NewService(a).RunMenuCompactAction(sessionKey, action)
}

func startThreadCompaction(a *App, sessionKey string) (*state.Session, error) {
	return appcompact.NewService(a).StartThreadCompaction(sessionKey)
}

func bindStandaloneCompactTurn(a *App, threadID, turnID string) bool {
	return appcompact.NewService(a).BindStandaloneCompactTurn(threadID, turnID)
}

func noteStandaloneCompactItemStarted(a *App, threadID, turnID string, item map[string]any) bool {
	return appcompact.NewService(a).NoteStandaloneCompactItemStarted(threadID, turnID, item)
}

func completeStandaloneCompactTurn(a *App, threadID, turnID string) bool {
	return appcompact.NewService(a).CompleteStandaloneCompactTurn(threadID, turnID)
}

func completeStandaloneCompactItem(a *App, threadID, turnID string, item map[string]any) bool {
	return appcompact.NewService(a).CompleteStandaloneCompactItem(threadID, turnID, item)
}

func finishStandaloneCompactTurn(a *App, threadID, turnID, status string) bool {
	return appcompact.NewService(a).FinishStandaloneCompactTurn(threadID, turnID, status)
}

func failStandaloneCompactTurn(a *App, threadID, turnID, message string) bool {
	return appcompact.NewService(a).FailStandaloneCompactTurn(threadID, turnID, message)
}

func restoreStandaloneCompactSession(a *App, sessionKey, threadID, previousStatus string) {
	appcompact.NewService(a).RestoreStandaloneCompactSession(sessionKey, threadID, previousStatus)
}

func sendStandaloneCompactResult(a *App, sess *state.Session, status string) {
	text := appcompact.StandaloneCompactResultText(status)
	if text == "" {
		return
	}
	appcompact.NewService(a).SendSessionTextNotice(sess, text)
}

func standaloneCompactResultText(status string) string {
	return appcompact.StandaloneCompactResultText(status)
}
