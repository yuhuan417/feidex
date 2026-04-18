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
	if strings.TrimSpace(sess.ActiveTurnID) != "" || strings.TrimSpace(sess.ActiveSubmissionID) != "" {
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
	if msg == nil {
		return nil
	}
	if _, err := a.startThreadCompaction(a.makeSessionKey(msg)); err != nil {
		return err
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "已请求压缩当前线程上下文。", msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
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
		if strings.TrimSpace(sess.ActiveSubmissionID) != "" {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if currentTurnID := strings.TrimSpace(sess.ActiveTurnID); currentTurnID != "" && currentTurnID != turnID {
			continue
		}
		if strings.TrimSpace(sess.Status) != sessionStatusCompacting && strings.TrimSpace(sess.ActiveTurnID) != turnID {
			continue
		}
		if strings.TrimSpace(sess.ActiveTurnID) == turnID {
			return true
		}
		sess.ActiveTurnID = turnID
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
		if strings.TrimSpace(sess.ActiveSubmissionID) != "" {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if currentTurnID := strings.TrimSpace(sess.ActiveTurnID); currentTurnID != "" && turnID != "" && currentTurnID != turnID {
			continue
		}
		if strings.TrimSpace(sess.Status) != sessionStatusCompacting && strings.TrimSpace(sess.ActiveTurnID) == "" {
			continue
		}
		sess.ActiveTurnID = ""
		sess.Status = "idle"
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
		if strings.TrimSpace(sess.ActiveSubmissionID) != "" {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if strings.TrimSpace(sess.ActiveTurnID) != turnID {
			continue
		}
		sess.ActiveTurnID = ""
		sess.Status = "idle"
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
		if strings.TrimSpace(sess.ActiveSubmissionID) != "" {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if currentTurnID := strings.TrimSpace(sess.ActiveTurnID); currentTurnID != "" && turnID != "" && currentTurnID != turnID {
			continue
		}
		if strings.TrimSpace(sess.Status) != sessionStatusCompacting && strings.TrimSpace(sess.ActiveTurnID) == "" {
			continue
		}
		sess.ActiveTurnID = ""
		sess.Status = "idle"
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
	if strings.TrimSpace(sess.ActiveTurnID) != "" || strings.TrimSpace(sess.ActiveSubmissionID) != "" {
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
	if strings.TrimSpace(sess.ChatType) == "group" && a.cfg.Feishu.ReplyInThread && strings.TrimSpace(sess.RootMessageID) != "" {
		_ = a.feishu.ReplyText(ctx, sess.RootMessageID, text, true)
		return
	}
	if chatID := strings.TrimSpace(sess.ChatID); chatID != "" {
		_ = a.feishu.SendText(ctx, chatID, text)
	}
}
