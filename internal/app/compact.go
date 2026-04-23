package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

const sessionStatusCompacting = "compacting"

func isContextCompactionItem(item map[string]any) bool {
	return normalizeTurnItemType(stringValue(item["type"])) == "context_compaction"
}

func sessionHasActiveWork(sess *state.Session) bool {
	if sess == nil {
		return false
	}
	if sessionHasActiveOperations(sess) {
		return true
	}
	switch strings.TrimSpace(sess.Status) {
	case sessionStatusCompacting, "turn_starting":
		return true
	default:
		return false
	}
}

func (a *App) commandCompact(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /compact")
	}
	return a.handleBackendCompactCommand(msg)
}

func compactMenuButtons(sessionKey string, includeRetry bool) []feishu.Button {
	buttons := []feishu.Button{}
	if includeRetry {
		buttons = append(buttons, feishu.Button{
			Text: "重试",
			Type: "primary",
			Value: map[string]any{
				"action":        "menu.compact",
				"session_key":   sessionKey,
				"parent_action": "menu.tools",
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: "返回常用工具",
		Type: "default",
		Value: map[string]any{
			"action":      "menu.tools",
			"session_key": sessionKey,
		},
	})
	return buttons
}

func (a *App) renderCompactPreparingCard(sessionKey string) map[string]any {
	body := "正在请求当前线程上下文压缩，请稍候。\n\n这张卡片会自动刷新。"
	return a.feishu.SimpleStatusCard("压缩上下文", "blue", menuCardBody("menu.tools", body), compactMenuButtons(sessionKey, false))
}

func (a *App) renderCompactAcceptedCard(sessionKey string) map[string]any {
	body := "已提交 `/compact`。\n\n后续结果会通过正常消息流返回。"
	return a.feishu.SimpleStatusCard("压缩上下文", "green", menuCardBody("menu.tools", body), compactMenuButtons(sessionKey, false))
}

func (a *App) renderCompactFailedCard(sessionKey, errText string) map[string]any {
	body := "请求 `/compact` 失败。"
	if text := strings.TrimSpace(errText); text != "" {
		body += "\n\n错误: " + text
	}
	return a.feishu.SimpleStatusCard("压缩上下文", "orange", menuCardBody("menu.tools", body), compactMenuButtons(sessionKey, true))
}

func (a *App) runMenuCompactAction(action *feishu.CardAction, sessionKey string) error {
	if a == nil {
		return nil
	}
	msg := a.commandMessageFromAction(action, sessionKey, "/compact")
	sessionKey = firstNonEmpty(a.makeSessionKey(msg), strings.TrimSpace(sessionKey))
	if a.isClaudeBackend() {
		return a.enqueueSubmission(msg)
	}
	_, err := a.startThreadCompaction(sessionKey)
	return err
}

func (a *App) startThreadCompaction(sessionKey string) (*state.Session, error) {
	if a == nil || a.store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	appState := a.appState()
	sess := appState.session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程，无法压缩上下文")
	}
	if sessionHasActiveWork(sess) {
		return nil, fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	previousStatus := strings.TrimSpace(sess.Status)
	sess.Status = sessionStatusCompacting
	if err := appState.saveSession(sess); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	if err := a.codex.Call(ctx, "thread/compact/start", map[string]any{
		"threadId": threadID,
	}, nil); err != nil {
		a.restoreStandaloneCompactSession(sessionKey, threadID, previousStatus)
		return nil, err
	}
	return appState.session(sessionKey), nil
}

func (a *App) bindStandaloneCompactTurn(threadID, turnID string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if a == nil || a.store == nil || threadID == "" || turnID == "" {
		return false
	}
	appState := a.appState()
	for _, sess := range appState.sessions() {
		if sess == nil {
			continue
		}
		if currentTurn := sessionFindActiveOperationByTurn(sess, turnID); currentTurn != nil && strings.TrimSpace(currentTurn.SubmissionID) == "" {
			return true
		}
		if sessionHasInFlightSubmission(sess) {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if currentTurn := sessionForegroundOperation(sess); currentTurn != nil && strings.TrimSpace(currentTurn.TurnID) != "" && strings.TrimSpace(currentTurn.TurnID) != turnID {
			continue
		}
		if strings.TrimSpace(sess.Status) != sessionStatusCompacting {
			continue
		}
		sessionUpsertActiveOperation(sess, state.SessionActiveOperation{
			Kind:     sessionOpKindTurn,
			ThreadID: threadID,
			TurnID:   turnID,
		})
		sess.Status = sessionStatusCompacting
		return appState.saveSession(sess) == nil
	}
	return false
}

func (a *App) noteStandaloneCompactItemStarted(threadID, turnID string, item map[string]any) bool {
	if !isContextCompactionItem(item) {
		return false
	}
	return a.bindStandaloneCompactTurn(threadID, turnID)
}

func (a *App) completeStandaloneCompactTurn(threadID, turnID string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if a == nil || a.store == nil || threadID == "" {
		return false
	}
	appState := a.appState()
	for _, sess := range appState.sessions() {
		if sess == nil {
			continue
		}
		if op := sessionFindActiveOperationByTurn(sess, turnID); op != nil && strings.TrimSpace(op.SubmissionID) != "" {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if turnID != "" && sessionFindActiveOperationByTurn(sess, turnID) == nil && sessionHasActiveOperations(sess) {
			continue
		}
		if strings.TrimSpace(sess.Status) != sessionStatusCompacting && sessionFindActiveOperationByThread(sess, threadID) == nil {
			continue
		}
		resolvedTurnID := strings.TrimSpace(turnID)
		if resolvedTurnID == "" {
			if op := sessionFindActiveOperationByThread(sess, threadID); op != nil && strings.TrimSpace(op.SubmissionID) == "" {
				resolvedTurnID = strings.TrimSpace(op.TurnID)
			}
		}
		sessionRemoveActiveOperation(sess, "", resolvedTurnID)
		if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
			sess.Status = "queued"
		} else {
			sess.Status = "idle"
		}
		if err := appState.saveSession(sess); err != nil {
			return false
		}
		a.sendStandaloneCompactResult(sess, "completed")
		return true
	}
	return false
}

func (a *App) completeStandaloneCompactItem(threadID, turnID string, item map[string]any) bool {
	if !isContextCompactionItem(item) {
		return false
	}
	switch normalizeWorkingStatus(firstNonEmpty(stringValue(item["status"]), stringValue(item["state"]))) {
	case "", "completed":
		return a.completeStandaloneCompactTurn(threadID, turnID)
	case "interrupted", "cancelled", "canceled":
		return a.finishStandaloneCompactTurn(threadID, turnID, "interrupted")
	default:
		return false
	}
}

func (a *App) finishStandaloneCompactTurn(threadID, turnID, status string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if a == nil || a.store == nil || threadID == "" || turnID == "" {
		return false
	}
	appState := a.appState()
	for _, sess := range appState.sessions() {
		if sess == nil {
			continue
		}
		if op := sessionFindActiveOperationByTurn(sess, turnID); op != nil && strings.TrimSpace(op.SubmissionID) != "" {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if sessionFindActiveOperationByTurn(sess, turnID) == nil {
			continue
		}
		sessionRemoveActiveOperation(sess, "", turnID)
		if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
			sess.Status = "queued"
		} else {
			sess.Status = "idle"
		}
		if err := appState.saveSession(sess); err != nil {
			return false
		}
		a.sendStandaloneCompactResult(sess, status)
		return true
	}
	return false
}

func (a *App) failStandaloneCompactTurn(threadID, turnID, message string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	message = strings.TrimSpace(message)
	if a == nil || a.store == nil || threadID == "" {
		return false
	}
	appState := a.appState()
	for _, sess := range appState.sessions() {
		if sess == nil {
			continue
		}
		if op := sessionFindActiveOperationByTurn(sess, turnID); op != nil && strings.TrimSpace(op.SubmissionID) != "" {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if turnID != "" && sessionFindActiveOperationByTurn(sess, turnID) == nil && sessionHasActiveOperations(sess) {
			continue
		}
		if strings.TrimSpace(sess.Status) != sessionStatusCompacting && sessionFindActiveOperationByThread(sess, threadID) == nil {
			continue
		}
		resolvedTurnID := strings.TrimSpace(turnID)
		if resolvedTurnID == "" {
			if op := sessionFindActiveOperationByThread(sess, threadID); op != nil && strings.TrimSpace(op.SubmissionID) == "" {
				resolvedTurnID = strings.TrimSpace(op.TurnID)
			}
		}
		sessionRemoveActiveOperation(sess, "", resolvedTurnID)
		if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
			sess.Status = "queued"
		} else {
			sess.Status = "idle"
		}
		if err := appState.saveSession(sess); err != nil {
			return false
		}
		text := "当前线程上下文压缩失败。"
		if message != "" {
			text = "当前线程上下文压缩失败：" + message
		}
		a.sendSessionTextNotice(sess, text)
		return true
	}
	return false
}

func (a *App) restoreStandaloneCompactSession(sessionKey, threadID, previousStatus string) {
	if a == nil || a.store == nil {
		return
	}
	appState := a.appState()
	sess := appState.session(sessionKey)
	if sess == nil {
		return
	}
	if strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return
	}
	if sessionHasActiveOperations(sess) {
		return
	}
	if strings.TrimSpace(sess.Status) != sessionStatusCompacting {
		return
	}
	sess.Status = strings.TrimSpace(previousStatus)
	if sess.Status == "" {
		sess.Status = "idle"
	}
	_ = appState.saveSession(sess)
}

func (a *App) sendStandaloneCompactResult(sess *state.Session, status string) {
	text := standaloneCompactResultText(status)
	if text == "" {
		return
	}
	a.sendSessionTextNotice(sess, text)
}

func standaloneCompactResultText(status string) string {
	switch strings.TrimSpace(status) {
	case "completed":
		return "当前线程上下文已压缩完成。"
	case "interrupted":
		return "当前线程上下文压缩已中断。"
	case "failed":
		return "当前线程上下文压缩失败。"
	default:
		return ""
	}
}

func (a *App) sendSessionTextNotice(sess *state.Session, text string) {
	if a == nil || a.feishu == nil || sess == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.TrimSpace(sess.ChatType) == "group" && a.replyInThreadEnabled(sess.ChatType) && strings.TrimSpace(sess.RootMessageID) != "" {
		_ = a.feishu.ReplyText(ctx, sess.RootMessageID, text, true)
		return
	}
	if chatID := strings.TrimSpace(sess.ChatID); chatID != "" {
		_ = a.feishu.SendText(ctx, chatID, text)
	}
}
