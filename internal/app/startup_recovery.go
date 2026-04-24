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

func recoverRuntimeState(a *App) {
	recoverSharedRuntimeState(a)
	recoverFrontendRuntimeState(a)
}

func recoverSharedRuntimeState(a *App) {
	resetLiveThreadState(a)
	appState := appState(a)
	sessions := appState.sessions()
	cleared := 0
	for _, sess := range sessions {
		if strings.TrimSpace(sess.WorkspaceID) == "" {
			sess.WorkspaceID = defaultWorkspaceID(a)
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
	newRuntimeMaintenanceService(a).expirePendingRequestsOnStartup()
	newRuntimeMaintenanceService(a).cleanupExpiredAttachments()
}

func recoverFrontendRuntimeState(a *App) {
	if a == nil {
		return
	}
	a.frontendRecoveryMu.Lock()
	defer a.frontendRecoveryMu.Unlock()
	resetLiveThreadState(a)
	if !hasConfiguredBackend(a) {
		return
	}
	endBackendRecovery := func() {}
	if runtime := backendRuntime(a); runtime != nil {
		endBackendRecovery = runtime.beginStartupRecoveryScope(a)
	}
	defer endBackendRecovery()
	recoverSessionThreadsOnStartup(a)
}

func resetLiveThreadState(a *App) {
	if a == nil {
		return
	}
	a.liveThreads = newLiveThreadTracker()
}

func recoverSessionThreadsOnStartup(a *App) {
	if a == nil || a.store == nil {
		return
	}
	appState := appState(a)
	effectiveModel := configuredGlobalModel(a.cfg)
	for _, sess := range appState.sessions() {
		if sess == nil {
			continue
		}
		if !sessionBelongsToFrontend(a, sess.Key) {
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
		workspaceID := firstNonEmpty(sess.ActiveThreadWorkspaceID, sess.WorkspaceID, defaultWorkspaceID(a))
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
			clearSessionLiveThread(a, sessionKey)
			continue
		}
		conversationBackend(a).recoverStartupConversation(sessionKey, workspaceID, sess, ws, effectiveModel)
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

func appStartupReadyChatIDs(a *App, sessions []*state.Session) []string {
	if a == nil {
		return startupReadyChatIDs(sessions)
	}
	filtered := make([]*state.Session, 0, len(sessions))
	for _, sess := range sessions {
		if sess == nil || !sessionBelongsToFrontend(a, sess.Key) {
			continue
		}
		filtered = append(filtered, sess)
	}
	return startupReadyChatIDs(filtered)
}

func sendStartupReadyNotifications(a *App) {
	if a == nil || a.feishu == nil || a.store == nil {
		return
	}
	chatIDs := appStartupReadyChatIDs(a, appState(a).sessions())
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
