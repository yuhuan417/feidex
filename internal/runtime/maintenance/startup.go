package maintenance

import (
	"context"
	"feidex/internal/domain/conversation"
	"feidex/internal/textutil"
	"log/slog"
	"sort"
	"strings"
	"time"

	"feidex/internal/domain/identity"
	"feidex/internal/domain/workspace"
	"sync"
)

func (s StartupRecovery) RecoverRuntimeState() {
	s.RecoverSharedRuntimeState()
	s.RecoverFrontendRuntimeState()
}

func (s StartupRecovery) RecoverSharedRuntimeState() {
	if s.deps.Repository == nil {
		return
	}
	s.deps.ResetLiveThreads()
	appState := s.deps.Repository
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
			sess.WorkspaceID = s.deps.DefaultWorkspaceID()
			slog.Warn("repairing empty workspace on startup",
				"session_key", sess.Key,
				"workspace_id", sess.WorkspaceID,
			)
		}
		if !conversation.HasInFlightSubmission(sess) && len(sess.Queue) == 0 && len(sess.StagedImages) == 0 && conversation.NormalizeSessionStatus(sess.Status) == conversation.SessionStatusIdle {
			if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
				conversation.ClearThreadContext(sess)
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
		conversation.ResetActiveOperations(sess)
		sess.Queue = nil
		sess.StagedImages = nil
		sess.Status = conversation.SessionStatusIdle.String()
		if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
			conversation.ClearThreadContext(sess)
		}
		_ = appState.SaveSession(sess)
		cleared++
	}
	if cleared > 0 {
		slog.Debug("runtime session state recovery complete", "cleared_sessions", cleared)
	}
	s.deps.ExpireRequests()
	s.deps.CleanupAttachments()
}

func (s StartupRecovery) RecoverFrontendRuntimeState() {
	if s.deps.Repository == nil {
		return
	}
	s.deps.RecoveryMu.Lock()
	defer s.deps.RecoveryMu.Unlock()
	{
		s.deps.ResetLiveThreads()
		if !s.deps.BackendConfigured() {
			return
		}
		endBackendRecovery := s.deps.BeginRecovery()
		if endBackendRecovery == nil {
			endBackendRecovery = func() {}
		}
		defer endBackendRecovery()
		s.recoverSessionThreadsOnStartup()
	}
}

func (s StartupRecovery) recoverSessionThreadsOnStartup() {
	if s.deps.Repository == nil {
		return
	}
	appState := s.deps.Repository
	if appState == nil {
		return
	}
	for _, sess := range appState.Sessions() {
		if sess == nil {
			continue
		}
		if !s.deps.BelongsToFrontend(sess.Key) {
			continue
		}
		if strings.TrimSpace(sess.ActiveThreadID) == "" {
			continue
		}
		if conversation.NormalizeSessionStatus(textutil.FirstNonEmpty(sess.Status, conversation.SessionStatusIdle.String())) != conversation.SessionStatusIdle {
			continue
		}
		if conversation.HasInFlightSubmission(sess) {
			continue
		}
		if len(sess.Queue) != 0 || len(sess.StagedImages) != 0 {
			continue
		}

		sessionKey := strings.TrimSpace(sess.Key)
		workspaceID := textutil.FirstNonEmpty(sess.ActiveThreadWorkspaceID, sess.WorkspaceID, s.deps.DefaultWorkspaceID())
		ws := s.deps.Workspace(workspaceID)
		if ws == nil {
			slog.Warn("startup thread recovery dropped unknown workspace lineage",
				"session_key", sessionKey,
				"thread_id", sess.ActiveThreadID,
				"workspace_id", workspaceID,
			)
			conversation.ClearThreadContext(sess)
			sess.Status = conversation.SessionStatusIdle.String()
			_ = appState.SaveSession(sess)
			s.deps.ClearLiveThread(sessionKey)
			continue
		}
		effectiveModel := s.deps.EffectiveModel(sess)
		s.deps.RecoverConversation(sessionKey, workspaceID, sess, ws, effectiveModel)
	}
}

func StartupReadyChatIDs(sessions []*conversation.Session) []string {
	seen := map[string]struct{}{}
	chatIDs := make([]string, 0, len(sessions))
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		chatType := strings.ToLower(strings.TrimSpace(sess.ChatType))
		chatID := strings.TrimSpace(sess.ChatID)
		if chatType == "" || chatID == "" {
			_, keyChatType, keyChatID, _, _ := identity.ParseSessionKey(sess.Key)
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

func (s StartupRecovery) FrontendStartupReadyChatIDs(sessions []*conversation.Session) []string {
	if s.deps.Repository == nil {
		return StartupReadyChatIDs(sessions)
	}
	filtered := make([]*conversation.Session, 0, len(sessions))
	for _, sess := range sessions {
		if sess == nil || !s.deps.BelongsToFrontend(sess.Key) {
			continue
		}
		filtered = append(filtered, sess)
	}
	return StartupReadyChatIDs(filtered)
}

func (s StartupRecovery) SendStartupReadyNotifications() {
	if s.deps.Repository == nil || s.deps.SendText == nil {
		return
	}
	appState := s.deps.Repository
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
		ctx, cancel := context.WithTimeout(s.context(), 10*time.Second)
		err := s.deps.SendText(ctx, chatID, text)
		cancel()
		if err != nil {
			slog.Error("startup ready notification failed", "chat_id", chatID, "error", err)
			continue
		}
		slog.Debug("startup ready notification sent", "chat_id", chatID)
	}
}

type SessionRepository interface {
	Sessions() []*conversation.Session
	SaveSession(*conversation.Session) error
}
type RecoveryDependencies struct {
	Context             func() context.Context
	Repository          SessionRepository
	RecoveryMu          *sync.Mutex
	DefaultWorkspaceID  func() string
	Workspace           func(string) *workspace.Workspace
	ResetLiveThreads    func()
	ClearLiveThread     func(string)
	BelongsToFrontend   func(string) bool
	BackendConfigured   func() bool
	BeginRecovery       func() func()
	EffectiveModel      func(*conversation.Session) string
	RecoverConversation func(string, string, *conversation.Session, *workspace.Workspace, string)
	ExpireRequests      func()
	CleanupAttachments  func()
	SendText            func(context.Context, string, string) error
}
type StartupRecovery struct{ deps RecoveryDependencies }

func NewStartupRecovery(deps RecoveryDependencies) StartupRecovery {
	return StartupRecovery{deps: deps}
}
func (s StartupRecovery) context() context.Context {
	if s.deps.Context != nil {
		return s.deps.Context()
	}
	return context.Background()
}
