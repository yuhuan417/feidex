package maintenance

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	appcore "feidex/internal/app/appcore"
	"feidex/internal/config"
	"feidex/internal/state"
)

func (s RuntimeMaintenanceService) RecoverRuntimeState() {
	s.RecoverSharedRuntimeState()
	s.RecoverFrontendRuntimeState()
}

func (s RuntimeMaintenanceService) RecoverSharedRuntimeState() {
	if s.app == nil {
		return
	}
	s.app.MaintenanceResetLiveThreadState()
	appState := s.app.MaintenanceAppState()
	if appState == nil {
		return
	}
	sessions := appState.Sessions()
	cleared := 0
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		if strings.TrimSpace(sess.WorkspaceID) == "" {
			sess.WorkspaceID = appcore.DefaultWorkspaceID(s.app)
			slog.Warn("repairing empty workspace on startup",
				"session_key", sess.Key,
				"workspace_id", sess.WorkspaceID,
			)
		}
		if !s.app.MaintenanceSessionHasInFlightSubmission(sess) && len(sess.Queue) == 0 && len(sess.StagedImages) == 0 && state.NormalizeSessionStatus(sess.Status) == state.SessionStatusIdle {
			if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
				s.app.MaintenanceClearSessionThreadContext(sess)
			}
			_ = appState.SaveSession(sess)
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
		s.app.MaintenanceResetSessionActiveOperations(sess)
		sess.Queue = nil
		sess.StagedImages = nil
		sess.Status = state.SessionStatusIdle.String()
		if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
			s.app.MaintenanceClearSessionThreadContext(sess)
		}
		_ = appState.SaveSession(sess)
		cleared++
	}
	if cleared > 0 {
		slog.Debug("runtime session state recovery complete", "cleared_sessions", cleared)
	}
	s.ExpirePendingRequestsOnStartup()
	s.CleanupExpiredAttachments()
}

func (s RuntimeMaintenanceService) RecoverFrontendRuntimeState() {
	if s.app == nil {
		return
	}
	s.app.MaintenanceWithFrontendRecoveryLock(func() {
		s.app.MaintenanceResetLiveThreadState()
		if !appcore.HasConfiguredBackend(s.app) {
			return
		}
		endBackendRecovery := s.app.MaintenanceBeginBackendStartupRecovery()
		if endBackendRecovery == nil {
			endBackendRecovery = func() {}
		}
		defer endBackendRecovery()
		s.recoverSessionThreadsOnStartup()
	})
}

func (s RuntimeMaintenanceService) recoverSessionThreadsOnStartup() {
	if s.app == nil {
		return
	}
	appState := s.app.MaintenanceAppState()
	if appState == nil {
		return
	}
	effectiveModel := s.app.MaintenanceConfiguredGlobalModel()
	for _, sess := range appState.Sessions() {
		if sess == nil {
			continue
		}
		if !s.app.MaintenanceSessionBelongsToFrontend(sess.Key) {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) == "" {
			continue
		}
		if state.NormalizeSessionStatus(firstNonEmpty(sess.Status, state.SessionStatusIdle.String())) != state.SessionStatusIdle {
			continue
		}
		if s.app.MaintenanceSessionHasInFlightSubmission(sess) {
			continue
		}
		if len(sess.Queue) != 0 || len(sess.StagedImages) != 0 {
			continue
		}

		sessionKey := strings.TrimSpace(sess.Key)
		workspaceID := firstNonEmpty(sess.ActiveThreadWorkspaceID, sess.WorkspaceID, appcore.DefaultWorkspaceID(s.app))
		ws := config.FindWorkspace(s.app.Config(), workspaceID)
		if ws == nil {
			slog.Warn("startup thread recovery dropped unknown workspace lineage",
				"session_key", sessionKey,
				"thread_id", sess.ActiveThreadID,
				"workspace_id", workspaceID,
			)
			s.app.MaintenanceClearSessionThreadContext(sess)
			sess.Status = state.SessionStatusIdle.String()
			_ = appState.SaveSession(sess)
			s.app.MaintenanceClearSessionLiveThread(sessionKey)
			continue
		}
		s.app.MaintenanceRecoverStartupConversation(sessionKey, workspaceID, sess, ws, effectiveModel)
	}
}

func StartupReadyChatIDs(sessions []*state.Session) []string {
	seen := map[string]struct{}{}
	chatIDs := make([]string, 0, len(sessions))
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		chatType := strings.ToLower(strings.TrimSpace(sess.ChatType))
		chatID := strings.TrimSpace(sess.ChatID)
		if chatType == "" || chatID == "" {
			_, keyChatType, keyChatID, _, _ := appcore.ParseSessionKey(sess.Key)
			if chatType == "" {
				chatType = keyChatType
			}
			if chatID == "" {
				chatID = keyChatID
			}
		}
		if chatType != "p2p" {
			continue
		}
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

func (s RuntimeMaintenanceService) FrontendStartupReadyChatIDs(sessions []*state.Session) []string {
	if s.app == nil {
		return StartupReadyChatIDs(sessions)
	}
	filtered := make([]*state.Session, 0, len(sessions))
	for _, sess := range sessions {
		if sess == nil || !s.app.MaintenanceSessionBelongsToFrontend(sess.Key) {
			continue
		}
		filtered = append(filtered, sess)
	}
	return StartupReadyChatIDs(filtered)
}

func (s RuntimeMaintenanceService) SendStartupReadyNotifications() {
	if s.app == nil || s.app.Feishu() == nil || s.app.Store() == nil {
		return
	}
	appState := s.app.MaintenanceAppState()
	if appState == nil {
		return
	}
	chatIDs := s.FrontendStartupReadyChatIDs(appState.Sessions())
	if len(chatIDs) == 0 {
		slog.Debug("startup ready notification skipped", "reason", "no_known_chats")
		return
	}
	const text = "feidex 已就绪，可继续发送消息。"
	for _, chatID := range chatIDs {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := s.app.Feishu().SendText(ctx, chatID, text)
		cancel()
		if err != nil {
			slog.Error("startup ready notification failed", "chat_id", chatID, "error", err)
			continue
		}
		slog.Debug("startup ready notification sent", "chat_id", chatID)
	}
}
