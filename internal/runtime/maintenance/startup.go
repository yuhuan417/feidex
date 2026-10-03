package maintenance

import (
	"context"
	"feidex/internal/domain/conversation"
	"log/slog"
	"sort"
	"strings"
	"time"

	"feidex/internal/domain/identity"
	"sync"
)

func (s StartupRecovery) RecoverRuntimeState() error {
	if err := s.ResetStartupState(); err != nil {
		return err
	}
	return s.RecoverFrontendRuntimeState()
}

func (s StartupRecovery) ResetStartupState() error {
	if s.deps.Repository == nil {
		return nil
	}
	s.deps.ResetLiveThreads()
	if err := s.deps.ResetState(); err != nil {
		return err
	}
	s.deps.CleanupAttachments()
	return nil
}

func (s StartupRecovery) RecoverFrontendRuntimeState() error {
	if s.deps.Repository == nil {
		return nil
	}
	s.deps.RecoveryMu.Lock()
	defer s.deps.RecoveryMu.Unlock()
	{
		s.deps.ResetLiveThreads()
		if !s.deps.BackendConfigured() {
			return nil
		}
		endBackendRecovery := s.deps.BeginRecovery()
		if endBackendRecovery == nil {
			endBackendRecovery = func() {}
		}
		defer endBackendRecovery()
		return s.deps.RestoreState()
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
	Context            func() context.Context
	Repository         SessionRepository
	RecoveryMu         *sync.Mutex
	ResetLiveThreads   func()
	BelongsToFrontend  func(string) bool
	BackendConfigured  func() bool
	BeginRecovery      func() func()
	ResetState         func() error
	RestoreState       func() error
	CleanupAttachments func()
	SendText           func(context.Context, string, string) error
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
