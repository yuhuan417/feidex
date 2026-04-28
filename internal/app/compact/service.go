// Package compact provides the context compaction service extracted from the
// app god package.
package compact

import (
	"context"
	"fmt"
	"strings"
	"time"

	appcore "feidex/internal/app/appcore"
	apputil "feidex/internal/app/apputil"
	"feidex/internal/app/sessionctx"
	"feidex/internal/app/turnitem"
	"feidex/internal/app/turn"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// SessionStatusCompacting is the session status indicating a compaction is in progress.
const SessionStatusCompacting = "compacting"

// ---------------------------------------------------------------------------
// App interface — what the service needs from the host application
// ---------------------------------------------------------------------------

// App defines the interface the compact service requires from the host
// application.
type App interface {
	appcore.AppConfig

	// Feishu returns the Feishu bot client.
	Feishu() appcore.FeishuClient
	// Codex returns the Codex RPC client.
	Codex() appcore.CodexClient
	// MenuCardBody formats a menu card body with breadcrumb navigation.
	MenuCardBody(action, body string) string
	// SessionStore returns the session persistence provider.
	SessionStore() SessionStore
	// HandleBackendCompactCommand dispatches /compact through the active
	// backend's compact command handler.
	HandleBackendCompactCommand(msg *feishu.InboundMessage) error
	// RunBackendCompactAction dispatches a compact menu action through the
	// active backend. The compact service is passed so the backend
	// implementation can call StartThreadCompaction without an import cycle.
	// action is the originating card action (may be nil for command-initiated
	// compactions).
	RunBackendCompactAction(sessionKey string, svc *Service, action any) error
}

// ---------------------------------------------------------------------------
// Session store provider
// ---------------------------------------------------------------------------

// SessionStore abstracts session persistence for the compact service.
type SessionStore interface {
	GetSession(key string) *state.Session
	AllSessions() []*state.Session
	SaveSession(sess *state.Session) error
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

// IsContextCompactionItem reports whether item is a context compaction turn item.
func IsContextCompactionItem(item map[string]any) bool {
	return turnitem.NormalizeTurnItemType(apputil.StringValue(item["type"])) == "context_compaction"
}

func normalizeWorkingStatus(v any) string {
	return turn.NormalizeWorkingStatus(v)
}

// SessionHasActiveWork reports whether the session has active work.
// This is the canonical implementation; app/ provides a thin wrapper.
func SessionHasActiveWork(sess *state.Session) bool {
	if sess == nil {
		return false
	}
	if sessionctx.HasActiveOperations(sess) {
		return true
	}
	switch state.NormalizeSessionStatus(sess.Status) {
	case state.SessionStatusCompacting, state.SessionStatusTurnStarting:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Service — manages the compact lifecycle
// ---------------------------------------------------------------------------

// Service manages context compaction for a single app instance.
type Service struct {
	app App
}

// NewService creates a new compact service bound to the given app.
func NewService(app App) Service {
	return Service{app: app}
}

// CommandCompact handles the /compact command.
func (s Service) CommandCompact(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /compact")
	}
	if s.app == nil {
		return nil
	}
	// Dispatch through the backend-specific compact command handler.
	// The app package wires the backend action facade; here we call through
	// the App interface which the concrete app implements.
	return s.app.HandleBackendCompactCommand(msg)
}

// CompactMenuButtons builds the buttons for compact result cards.
func CompactMenuButtons(sessionKey string, includeRetry bool) []feishu.Button {
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

// RenderCompactPreparingCard builds the "preparing" card for compact action.
func (s Service) RenderCompactPreparingCard(sessionKey string) map[string]any {
	body := "正在请求当前线程上下文压缩，请稍候。\n\n这张卡片会自动刷新。"
	return s.app.Feishu().SimpleStatusCard("压缩上下文", "blue", s.app.MenuCardBody("menu.tools", body), CompactMenuButtons(sessionKey, false))
}

// RenderCompactAcceptedCard builds the "accepted" card for compact action.
func (s Service) RenderCompactAcceptedCard(sessionKey string) map[string]any {
	body := "已提交 `/compact`。\n\n后续结果会通过正常消息流返回。"
	return s.app.Feishu().SimpleStatusCard("压缩上下文", "green", s.app.MenuCardBody("menu.tools", body), CompactMenuButtons(sessionKey, false))
}

// RenderCompactFailedCard builds the "failed" card for compact action.
func (s Service) RenderCompactFailedCard(sessionKey, errText string) map[string]any {
	body := "请求 `/compact` 失败。"
	if text := strings.TrimSpace(errText); text != "" {
		body += "\n\n错误: " + text
	}
	return s.app.Feishu().SimpleStatusCard("压缩上下文", "orange", s.app.MenuCardBody("menu.tools", body), CompactMenuButtons(sessionKey, true))
}

// RunMenuCompactAction dispatches the compact menu action through the backend.
// action is the originating card action (may be nil for command-initiated
// compactions).
func (s Service) RunMenuCompactAction(sessionKey string, action any) error {
	if s.app == nil {
		return nil
	}
	svc := s
	return s.app.RunBackendCompactAction(sessionKey, &svc, action)
}

// StartThreadCompaction starts a context compaction on the active thread.
func (s Service) StartThreadCompaction(sessionKey string) (*state.Session, error) {
	if s.app == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	store := s.app.SessionStore()
	if store == nil {
		return nil, fmt.Errorf("store not initialized")
	}
	sess := store.GetSession(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程，无法压缩上下文")
	}
	if SessionHasActiveWork(sess) {
		return nil, fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	previousStatus := strings.TrimSpace(sess.Status)
	sess.Status = state.SessionStatusCompacting.String()
	if err := store.SaveSession(sess); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	client := s.app.Codex()
	if client == nil {
		RestoreSession(store, sessionKey, threadID, previousStatus)
		return nil, fmt.Errorf("codex client not initialized")
	}
	if err := client.Call(ctx, "thread/compact/start", map[string]any{
		"threadId": threadID,
	}, nil); err != nil {
		RestoreSession(store, sessionKey, threadID, previousStatus)
		return nil, err
	}
	return store.GetSession(sessionKey), nil
}

// BindStandaloneCompactTurn binds a compact turn to a session.
func (s Service) BindStandaloneCompactTurn(threadID, turnID string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if s.app == nil || threadID == "" || turnID == "" {
		return false
	}
	store := s.app.SessionStore()
	if store == nil {
		return false
	}
	for _, sess := range store.AllSessions() {
		if sess == nil {
			continue
		}
		if currentTurn := sessionctx.FindActiveOperationByTurn(sess, turnID); currentTurn != nil && strings.TrimSpace(currentTurn.SubmissionID) == "" {
			return true
		}
		if sessionctx.HasInFlightSubmission(sess) {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if currentTurn := sessionctx.ForegroundOperation(sess); currentTurn != nil && strings.TrimSpace(currentTurn.TurnID) != "" && strings.TrimSpace(currentTurn.TurnID) != turnID {
			continue
		}
		if state.NormalizeSessionStatus(sess.Status) != state.SessionStatusCompacting {
			continue
		}
		sessionctx.UpsertActiveOperation(sess, state.SessionActiveOperation{
			Kind:     sessionctx.OpKindTurn,
			ThreadID: threadID,
			TurnID:   turnID,
		})
		sess.Status = state.SessionStatusCompacting.String()
		return store.SaveSession(sess) == nil
	}
	return false
}

// NoteStandaloneCompactItemStarted records that a compact item has started.
func (s Service) NoteStandaloneCompactItemStarted(threadID, turnID string, item map[string]any) bool {
	if !IsContextCompactionItem(item) {
		return false
	}
	return s.BindStandaloneCompactTurn(threadID, turnID)
}

// CompleteStandaloneCompactTurn completes a standalone compact turn.
func (s Service) CompleteStandaloneCompactTurn(threadID, turnID string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if s.app == nil || threadID == "" {
		return false
	}
	store := s.app.SessionStore()
	if store == nil {
		return false
	}
	for _, sess := range store.AllSessions() {
		if sess == nil {
			continue
		}
		if op := sessionctx.FindActiveOperationByTurn(sess, turnID); op != nil && strings.TrimSpace(op.SubmissionID) != "" {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if turnID != "" && sessionctx.FindActiveOperationByTurn(sess, turnID) == nil && sessionctx.HasActiveOperations(sess) {
			continue
		}
		if state.NormalizeSessionStatus(sess.Status) != state.SessionStatusCompacting && sessionctx.FindActiveOperationByThread(sess, threadID) == nil {
			continue
		}
		resolvedTurnID := strings.TrimSpace(turnID)
		if resolvedTurnID == "" {
			if op := sessionctx.FindActiveOperationByThread(sess, threadID); op != nil && strings.TrimSpace(op.SubmissionID) == "" {
				resolvedTurnID = strings.TrimSpace(op.TurnID)
			}
		}
		sessionctx.RemoveActiveOperation(sess, "", resolvedTurnID)
		if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
			sess.Status = state.SessionStatusQueued.String()
		} else {
			sess.Status = state.SessionStatusIdle.String()
		}
		if err := store.SaveSession(sess); err != nil {
			return false
		}
		s.sendStandaloneCompactResult(sess, "completed")
		return true
	}
	return false
}

// CompleteStandaloneCompactItem completes a standalone compact item if it is a
// context compaction item.
func (s Service) CompleteStandaloneCompactItem(threadID, turnID string, item map[string]any) bool {
	if !IsContextCompactionItem(item) {
		return false
	}
	switch normalizeWorkingStatus(apputil.FirstNonEmpty(apputil.StringValue(item["status"]), apputil.StringValue(item["state"]))) {
	case "", "completed":
		return s.CompleteStandaloneCompactTurn(threadID, turnID)
	case "interrupted", "cancelled", "canceled":
		return s.FinishStandaloneCompactTurn(threadID, turnID, "interrupted")
	default:
		return false
	}
}

// FinishStandaloneCompactTurn finishes a standalone compact turn with the
// given status.
func (s Service) FinishStandaloneCompactTurn(threadID, turnID, status string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if s.app == nil || threadID == "" || turnID == "" {
		return false
	}
	store := s.app.SessionStore()
	if store == nil {
		return false
	}
	for _, sess := range store.AllSessions() {
		if sess == nil {
			continue
		}
		if op := sessionctx.FindActiveOperationByTurn(sess, turnID); op != nil && strings.TrimSpace(op.SubmissionID) != "" {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if sessionctx.FindActiveOperationByTurn(sess, turnID) == nil {
			continue
		}
		sessionctx.RemoveActiveOperation(sess, "", turnID)
		if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
			sess.Status = state.SessionStatusQueued.String()
		} else {
			sess.Status = state.SessionStatusIdle.String()
		}
		if err := store.SaveSession(sess); err != nil {
			return false
		}
		s.sendStandaloneCompactResult(sess, status)
		return true
	}
	return false
}

// FailStandaloneCompactTurn fails a standalone compact turn with the given
// message.
func (s Service) FailStandaloneCompactTurn(threadID, turnID, message string) bool {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	message = strings.TrimSpace(message)
	if s.app == nil || threadID == "" {
		return false
	}
	store := s.app.SessionStore()
	if store == nil {
		return false
	}
	for _, sess := range store.AllSessions() {
		if sess == nil {
			continue
		}
		if op := sessionctx.FindActiveOperationByTurn(sess, turnID); op != nil && strings.TrimSpace(op.SubmissionID) != "" {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) != threadID {
			continue
		}
		if turnID != "" && sessionctx.FindActiveOperationByTurn(sess, turnID) == nil && sessionctx.HasActiveOperations(sess) {
			continue
		}
		if state.NormalizeSessionStatus(sess.Status) != state.SessionStatusCompacting && sessionctx.FindActiveOperationByThread(sess, threadID) == nil {
			continue
		}
		resolvedTurnID := strings.TrimSpace(turnID)
		if resolvedTurnID == "" {
			if op := sessionctx.FindActiveOperationByThread(sess, threadID); op != nil && strings.TrimSpace(op.SubmissionID) == "" {
				resolvedTurnID = strings.TrimSpace(op.TurnID)
			}
		}
		sessionctx.RemoveActiveOperation(sess, "", resolvedTurnID)
		if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
			sess.Status = state.SessionStatusQueued.String()
		} else {
			sess.Status = state.SessionStatusIdle.String()
		}
		if err := store.SaveSession(sess); err != nil {
			return false
		}
		text := "当前线程上下文压缩失败。"
		if message != "" {
			text = "当前线程上下文压缩失败：" + message
		}
		s.SendSessionTextNotice(sess, text)
		return true
	}
	return false
}

// RestoreSession restores a session to its previous status after a failed
// compaction start. It is a standalone helper that does not require a Service.
func RestoreSession(store SessionStore, sessionKey, threadID, previousStatus string) {
	if store == nil {
		return
	}
	sess := store.GetSession(sessionKey)
	if sess == nil {
		return
	}
	if strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return
	}
	if sessionctx.HasActiveOperations(sess) {
		return
	}
	if state.NormalizeSessionStatus(sess.Status) != state.SessionStatusCompacting {
		return
	}
	sess.Status = strings.TrimSpace(previousStatus)
	if sess.Status == "" {
		sess.Status = state.SessionStatusIdle.String()
	}
	_ = store.SaveSession(sess)
}

// RestoreStandaloneCompactSession restores a session to its previous status
// after a failed compaction start.
func (s Service) RestoreStandaloneCompactSession(sessionKey, threadID, previousStatus string) {
	if s.app == nil {
		return
	}
	store := s.app.SessionStore()
	RestoreSession(store, sessionKey, threadID, previousStatus)
}

// StandaloneCompactResultText returns the user-facing text for a compact result
// status.
func StandaloneCompactResultText(status string) string {
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

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (s Service) sendStandaloneCompactResult(sess *state.Session, status string) {
	text := StandaloneCompactResultText(status)
	if text == "" {
		return
	}
	s.SendSessionTextNotice(sess, text)
}

// SendSessionTextNotice sends a text notice to the session's chat.
func (s Service) SendSessionTextNotice(sess *state.Session, text string) {
	if s.app == nil || s.app.Feishu() == nil || sess == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.TrimSpace(sess.ChatType) == "group" && appcore.ReplyInThreadEnabled(s.app, sess.ChatType) && strings.TrimSpace(sess.RootMessageID) != "" {
		_ = s.app.Feishu().ReplyText(ctx, sess.RootMessageID, text, true)
		return
	}
	if chatID := strings.TrimSpace(sess.ChatID); chatID != "" {
		_ = s.app.Feishu().SendText(ctx, chatID, text)
	}
}
