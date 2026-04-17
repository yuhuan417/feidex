package app

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
)

func (a *App) recoverRuntimeState() {
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
		if sess.ActiveTurnID == "" && sess.ActiveSubmissionID == "" && len(sess.Queue) == 0 && len(sess.StagedImages) == 0 && sess.Status == "idle" {
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
		sess.ActiveTurnID = ""
		sess.ActiveSubmissionID = ""
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
		if strings.TrimSpace(sess.ActiveThreadID) == "" {
			continue
		}
		if strings.TrimSpace(firstNonEmpty(sess.Status, "idle")) != "idle" {
			continue
		}
		if strings.TrimSpace(sess.ActiveTurnID) != "" || strings.TrimSpace(sess.ActiveSubmissionID) != "" {
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

		threadID := strings.TrimSpace(sess.ActiveThreadID)
		resumeParams := map[string]any{
			"threadId":               threadID,
			"persistExtendedHistory": true,
		}
		if strings.TrimSpace(effectiveModel) != "" {
			resumeParams["model"] = effectiveModel
		}
		var resumeResp codexrpc.ThreadStartResult
		slog.Debug("startup thread resume request",
			"session_key", sessionKey,
			"thread_id", threadID,
			"workspace_id", workspaceID,
			"model", effectiveModel,
		)
		resumeCtx, resumeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := a.codex.Call(withCodexWorkspace(resumeCtx, workspaceID), "thread/resume", resumeParams, &resumeResp)
		resumeCancel()
		if err == nil {
			setSessionThreadContext(sess,
				workspaceID,
				firstNonEmpty(strings.TrimSpace(resumeResp.Thread.ID), threadID),
				firstNonEmpty(strings.TrimSpace(resumeResp.Thread.Name), sess.ActiveThreadName),
				firstNonEmpty(strings.TrimSpace(resumeResp.Thread.Preview), sess.ActiveThreadPreview),
			)
			sess.Status = "idle"
			if upsertErr := appState.saveSession(sess); upsertErr != nil {
				slog.Error("startup thread resume persistence failed",
					"session_key", sessionKey,
					"thread_id", sess.ActiveThreadID,
					"workspace_id", workspaceID,
					"error", upsertErr,
				)
				continue
			}
			a.markSessionThreadLive(sessionKey, sess.ActiveThreadID)
			slog.Debug("startup thread resumed",
				"session_key", sessionKey,
				"thread_id", sess.ActiveThreadID,
				"workspace_id", workspaceID,
				"model", effectiveModel,
			)
			continue
		}

		slog.Warn("startup thread/resume failed; starting fresh thread",
			"session_key", sessionKey,
			"thread_id", threadID,
			"workspace_id", workspaceID,
			"model", effectiveModel,
			"error", err,
		)
		threadParams := a.buildThreadStartParams(ws, sess, effectiveModel)
		var threadResp codexrpc.ThreadStartResult
		slog.Debug("startup thread start request",
			"session_key", sessionKey,
			"workspace_id", workspaceID,
			"cwd", ws.Cwd,
			"model", effectiveModel,
		)
		threadCtx, threadCancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = a.codex.Call(withCodexWorkspace(threadCtx, workspaceID), "thread/start", threadParams, &threadResp)
		threadCancel()
		if err != nil {
			slog.Error("startup thread/start failed; clearing thread lineage",
				"session_key", sessionKey,
				"stale_thread_id", threadID,
				"workspace_id", workspaceID,
				"cwd", ws.Cwd,
				"error", err,
			)
			clearSessionThreadContext(sess)
			sess.Status = "idle"
			_ = appState.saveSession(sess)
			a.clearSessionLiveThread(sessionKey)
			continue
		}
		setSessionThreadContext(sess, workspaceID, threadResp.Thread.ID, threadResp.Thread.Name, threadResp.Thread.Preview)
		sess.Status = "idle"
		if upsertErr := appState.saveSession(sess); upsertErr != nil {
			slog.Error("startup fresh thread persistence failed",
				"session_key", sessionKey,
				"thread_id", threadResp.Thread.ID,
				"workspace_id", workspaceID,
				"error", upsertErr,
			)
			continue
		}
		a.markSessionThreadLive(sessionKey, threadResp.Thread.ID)
		slog.Debug("startup thread started",
			"session_key", sessionKey,
			"thread_id", threadResp.Thread.ID,
			"workspace_id", workspaceID,
			"model", effectiveModel,
		)
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

func (a *App) sendStartupReadyNotifications() {
	if a == nil || a.feishu == nil || a.store == nil {
		return
	}
	chatIDs := startupReadyChatIDs(a.appState().sessions())
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
