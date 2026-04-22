package app

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/state"
)

func (a *App) recoverRuntimeState() {
	a.recoverSharedRuntimeState()
	a.recoverFrontendRuntimeState()
}

func (a *App) recoverSharedRuntimeState() {
	a.resetLiveThreadState()
	appState := a.appState()
	sessions := appState.sessions()
	cleared := 0
	for _, sess := range sessions {
		if strings.TrimSpace(sess.WorkspaceID) == "" {
			sess.WorkspaceID = a.defaultWorkspaceID()
			slog.Warn("repairing empty workspace on startup",
				"session_key", sess.Key,
				"workspace_id", sess.WorkspaceID,
			)
		}
		if !sessionHasInFlightSubmission(sess) && len(sess.Queue) == 0 && len(sess.StagedImages) == 0 && sess.Status == "idle" {
			if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
				clearSessionThreadContext(sess)
			}
			_ = appState.saveSession(sess)
			continue
		}
		slog.Warn("clearing stale runtime session state on startup",
			"session_key", sess.Key,
			"active_thread_id", sess.ActiveThreadID,
			"active_turn_id", sess.ActiveTurnID,
			"active_submission_id", sess.ActiveSubmissionID,
			"queue_len", len(sess.Queue),
			"status", sess.Status,
		)
		sessionResetActiveOperations(sess)
		sess.Queue = nil
		sess.StagedImages = nil
		sess.Status = "idle"
		if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
			clearSessionThreadContext(sess)
		}
		_ = appState.saveSession(sess)
		cleared++
	}
	if cleared > 0 {
		slog.Debug("runtime session state recovery complete", "cleared_sessions", cleared)
	}
	a.expirePendingRequestsOnStartup()
	a.cleanupExpiredAttachments()
}

func (a *App) recoverFrontendRuntimeState() {
	a.resetLiveThreadState()
	if !a.hasConfiguredBackend() {
		return
	}
	a.recoverSessionThreadsOnStartup()
}

func (a *App) resetLiveThreadState() {
	if a == nil {
		return
	}
	a.liveThreadMu.Lock()
	defer a.liveThreadMu.Unlock()
	a.liveThreads = map[string]string{}
}

func (a *App) recoverSessionThreadsOnStartup() {
	if a == nil || a.store == nil {
		return
	}
	appState := a.appState()
	effectiveModel := configuredGlobalModel(a.cfg)
	for _, sess := range appState.sessions() {
		if sess == nil {
			continue
		}
		if !a.sessionBelongsToFrontend(sess.Key) {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) == "" {
			continue
		}
		if strings.TrimSpace(firstNonEmpty(sess.Status, "idle")) != "idle" {
			continue
		}
		if sessionHasInFlightSubmission(sess) {
			continue
		}
		if len(sess.Queue) != 0 || len(sess.StagedImages) != 0 {
			continue
		}

		sessionKey := strings.TrimSpace(sess.Key)
		workspaceID := firstNonEmpty(sess.ActiveThreadWorkspaceID, sess.WorkspaceID, a.defaultWorkspaceID())
		ws := config.FindWorkspace(a.cfg, workspaceID)
		if ws == nil {
			slog.Warn("startup thread recovery dropped unknown workspace lineage",
				"session_key", sessionKey,
				"thread_id", sess.ActiveThreadID,
				"workspace_id", workspaceID,
			)
			clearSessionThreadContext(sess)
			sess.Status = "idle"
			_ = appState.saveSession(sess)
			a.clearSessionLiveThread(sessionKey)
			continue
		}
		a.conversationBackend().recoverStartupConversation(sessionKey, workspaceID, sess, ws, effectiveModel)
	}
}

func startupReadyChatIDs(sessions []*state.Session) []string {
	seen := map[string]struct{}{}
	chatIDs := make([]string, 0, len(sessions))
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		chatID := strings.TrimSpace(sess.ChatID)
		if chatID == "" {
			continue
		}
		if _, ok := seen[chatID]; ok {
			continue
		}
		seen[chatID] = struct{}{}
		chatIDs = append(chatIDs, chatID)
	}
	sort.Strings(chatIDs)
	return chatIDs
}

func (a *App) startupReadyChatIDs(sessions []*state.Session) []string {
	if a == nil {
		return startupReadyChatIDs(sessions)
	}
	filtered := make([]*state.Session, 0, len(sessions))
	for _, sess := range sessions {
		if sess == nil || !a.sessionBelongsToFrontend(sess.Key) {
			continue
		}
		filtered = append(filtered, sess)
	}
	return startupReadyChatIDs(filtered)
}

func (a *App) sendStartupReadyNotifications() {
	if a == nil || a.feishu == nil || a.store == nil {
		return
	}
	chatIDs := a.startupReadyChatIDs(a.appState().sessions())
	if len(chatIDs) == 0 {
		slog.Debug("startup ready notification skipped", "reason", "no_known_chats")
		return
	}
	const text = "feidex 已就绪，可继续发送消息。"
	for _, chatID := range chatIDs {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := a.feishu.SendText(ctx, chatID, text)
		cancel()
		if err != nil {
			slog.Error("startup ready notification failed", "chat_id", chatID, "error", err)
			continue
		}
		slog.Debug("startup ready notification sent", "chat_id", chatID)
	}
}
