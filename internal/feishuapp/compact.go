package feishuapp

import (
	compactview "feidex/internal/adapter/feishu/compaction"
	compaction "feidex/internal/application/compaction"
	"feidex/internal/domain/conversation"
)

const sessionStatusCompacting = "compacting"

func sessionHasActiveWork(sess *conversation.Session) bool { return conversation.HasActiveWork(sess) }
func compactCardTitle(a *App, key string) string {
	ws := ""
	if a != nil {
		if sess := a.State().Session(key); sess != nil {
			ws = sess.WorkspaceID
		}
	}
	return contentCardTitleForSession(a, key, ws, "压缩上下文")
}
func renderCompactPreparingCard(a *App, key string) map[string]any {
	return compactview.RenderCompactPreparingCard(compactCardTitle(a, key), key)
}
func renderCompactAcceptedCard(a *App, key string) map[string]any {
	return compactview.RenderCompactAcceptedCard(compactCardTitle(a, key), key)
}
func renderCompactFailedCard(a *App, key, text string) map[string]any {
	return compactview.RenderCompactFailedCard(compactCardTitle(a, key), key, text)
}
func sendStandaloneCompactResult(compactionDep *compaction.Service, sess *conversation.Session, status string) {
	compactionDep.SendSessionTextNotice(sess, compaction.StandaloneCompactResultText(status))
}
func standaloneCompactResultText(status string) string {
	return compaction.StandaloneCompactResultText(status)
}
