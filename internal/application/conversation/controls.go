package conversation

import (
	"context"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/workspace"
	"feidex/internal/textutil"
	"fmt"
	"sort"
	"strings"
	"time"
)

type ControlRepository interface {
	Session(string) *conversation.Session
	Sessions() []*conversation.Session
}
type PendingInputs interface{ DiscardSessionPendingInputs(string) int }
type RetryControl interface {
	Lock(string) func()
	Cancel(string, bool, string) bool
}
type RuntimeControl interface {
	Reconcile(string, *conversation.Session) *conversation.Session
	Interrupted(string, *conversation.Session) *conversation.Session
	Interrupt(context.Context, string, *conversation.Session) error
}
type ConversationControl interface {
	StartWorkspaceThread(string, *conversation.Session, *workspace.Workspace) (*conversation.ThreadBinding, error)
	ResumeSelectedThread(string, *conversation.Session, *workspace.Workspace, conversation.ThreadSelection) (*conversation.ThreadBinding, error)
	ContinueActiveTurn(string, string) error
}
type CommandContext struct{ SessionKey, UserID, ChatID, ChatType string }
type StopResult struct {
	Discarded                   int
	CancelledRetry, Interrupted bool
}
type FreshResult struct {
	Discarded int
	Binding   *conversation.ThreadBinding
}
type ControlDependencies struct {
	Repository    ControlRepository
	Workspaces    RecoveryWorkspaces
	Conversations ConversationControl
	Pending       PendingInputs
	Retry         RetryControl
	Runtime       RuntimeControl
	Context       func() context.Context
}

type Controls ControlDependencies

func NewControls(deps ControlDependencies) Controls { return Controls(deps) }

func (s Controls) session(input CommandContext) *conversation.Session {
	sess := s.Repository.Session(input.SessionKey)
	if sess == nil {
		sess = &conversation.Session{Key: input.SessionKey}
	}
	sess.WorkspaceID = textutil.FirstNonEmpty(sess.WorkspaceID, s.Workspaces.DefaultID())
	sess.OwnerUserID = textutil.FirstNonEmpty(sess.OwnerUserID, input.UserID)
	sess.ChatID = textutil.FirstNonEmpty(sess.ChatID, input.ChatID)
	sess.ChatType = textutil.FirstNonEmpty(sess.ChatType, input.ChatType)
	return sess
}

func (s Controls) Fresh(input CommandContext) (FreshResult, error) {
	if sess := s.Repository.Session(input.SessionKey); sess != nil && conversation.HasActiveWork(sess) {
		return FreshResult{}, fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	result := FreshResult{Discarded: s.Pending.DiscardSessionPendingInputs(input.SessionKey)}
	sess := s.session(input)
	ws := s.Workspaces.Get(sess.WorkspaceID)
	if ws == nil {
		return result, fmt.Errorf("workspace %q not found", sess.WorkspaceID)
	}
	var err error
	result.Binding, err = s.Conversations.StartWorkspaceThread(input.SessionKey, sess, ws)
	return result, err
}

func (s Controls) Resume(input CommandContext, selection conversation.ThreadSelection) error {
	sess := s.session(input)
	if conversation.HasInFlightSubmission(sess) {
		return conversation.NewWarning("当前任务仍在运行，请先等待结束或中断")
	}
	ws := s.Workspaces.Get(sess.WorkspaceID)
	if ws == nil {
		return fmt.Errorf("workspace not found")
	}
	_, err := s.Conversations.ResumeSelectedThread(input.SessionKey, sess, ws, selection)
	return err
}

func (s Controls) Append(key, text string) error {
	return s.Conversations.ContinueActiveTurn(key, text)
}

func groupChat(key string, sess *conversation.Session) (string, string) {
	_, kind, id, _, _ := identity.ParseSessionKey(key)
	if kind == "" && sess != nil {
		kind, id = sess.ChatType, sess.ChatID
	}
	return kind, id
}
func (s Controls) surfaceKeys(key string) []string {
	keys := []string{key}
	kind, chat := groupChat(key, s.Repository.Session(key))
	if kind == "group" && strings.TrimSpace(chat) != "" {
		for _, sess := range s.Repository.Sessions() {
			if sess == nil || sess.Key == key {
				continue
			}
			candidateKind, candidateChat := groupChat(sess.Key, sess)
			if candidateKind == "group" && candidateChat == chat {
				keys = append(keys, sess.Key)
			}
		}
	}
	sort.Strings(keys)
	return keys
}
func activeInterrupt(sess *conversation.Session) bool {
	return sess != nil && strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveTurnID) != ""
}
func (s Controls) target(keys []string) (string, *conversation.Session) {
	var best *conversation.Session
	var key string
	for _, candidate := range keys {
		sess := s.Repository.Session(candidate)
		if !activeInterrupt(sess) {
			continue
		}
		if best == nil || sess.UpdatedAt > best.UpdatedAt || (sess.UpdatedAt == best.UpdatedAt && candidate > key) {
			key, best = candidate, sess
		}
	}
	return key, best
}
func (s Controls) cancelRetries(keys []string, activeKey string, active *conversation.Session) bool {
	cancelled := false
	for _, key := range keys {
		sess := active
		if key != activeKey {
			sess = s.Repository.Session(key)
		}
		if s.Retry.Cancel(key, key == activeKey && activeInterrupt(sess), "已停止当前 session 的自动重试。") {
			cancelled = true
		}
	}
	return cancelled
}

func (s Controls) Stop(key string) (StopResult, error) {
	keys := s.surfaceKeys(key)
	for _, key := range keys {
		unlock := s.Retry.Lock(key)
		defer unlock()
	}
	var result StopResult
	for _, key := range keys {
		result.Discarded += s.Pending.DiscardSessionPendingInputs(key)
	}
	activeKey, sess := s.target(keys)
	result.CancelledRetry = s.cancelRetries(keys, activeKey, sess)
	if sess != nil {
		s.Runtime.Reconcile(activeKey, sess)
	}
	activeKey, sess = s.target(keys)
	result.CancelledRetry = s.cancelRetries(keys, activeKey, sess) || result.CancelledRetry
	if !activeInterrupt(sess) {
		if result.Discarded == 0 && !result.CancelledRetry {
			return result, fmt.Errorf("当前没有运行中的任务")
		}
		return result, nil
	}
	ctx, cancel := context.WithTimeout(s.Context(), 20*time.Second)
	defer cancel()
	if err := s.Runtime.Interrupt(ctx, activeKey, sess); err != nil {
		return result, err
	}
	s.Runtime.Interrupted(activeKey, sess)
	result.Interrupted = true
	return result, nil
}
