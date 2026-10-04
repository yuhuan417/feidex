package feishuapp

import (
	compactview "feidex/internal/adapter/feishu/compaction"
	"feidex/internal/adapter/feishu/planmode"
	compaction "feidex/internal/application/compaction"
	"feidex/internal/domain/conversation"
)

const sessionStatusCompacting = "compacting"

func sessionHasActiveWork(sess *conversation.Session) bool { return conversation.HasActiveWork(sess) }
func compactCardTitle(state planmode.SessionStateProvider, key string) string {
	ws := ""
	if state != nil {
		if sess := state.Session(key); sess != nil {
			ws = sess.WorkspaceID
		}
	}
	return planmode.ContentCardTitleForSessionFromState(state, state != nil, key, ws, "压缩上下文")
}
func renderCompactPreparingCard(state planmode.SessionStateProvider, key string) map[string]any {
	return compactview.RenderCompactPreparingCard(compactCardTitle(state, key), key)
}
func renderCompactAcceptedCard(state planmode.SessionStateProvider, key string) map[string]any {
	return compactview.RenderCompactAcceptedCard(compactCardTitle(state, key), key)
}
func renderCompactFailedCard(state planmode.SessionStateProvider, key, text string) map[string]any {
	return compactview.RenderCompactFailedCard(compactCardTitle(state, key), key, text)
}
func sendStandaloneCompactResult(compactionDep *compaction.Service, sess *conversation.Session, status string) {
	compactionDep.SendSessionTextNotice(sess, compaction.StandaloneCompactResultText(status))
}
func standaloneCompactResultText(status string) string {
	return compaction.StandaloneCompactResultText(status)
}
