package plan

import (
	"context"
	interactionapp "feidex/internal/application/interaction"
	"fmt"
	"strings"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/submission"
	"feidex/internal/domain/workspace"
	"feidex/internal/textutil"
)

type Repository interface {
	Session(string) *conversation.Session
	UpdateSession(string, func(*conversation.Session)) (*conversation.Session, error)
	Pending(string) *interaction.PendingRequest
	PendingRequests() []*interaction.PendingRequest
	UpdatePending(string, func(*interaction.PendingRequest)) error
	CreateSubmission(*submission.Submission) (string, error)
	QueueSubmission(string, string) error
	Submission(string) *submission.Submission
}
type Settings interface {
	Resolve(*conversation.Session, bool) (*conversation.SessionCollaborationMode, error)
	CaptureMode(*conversation.Session) *conversation.SessionCollaborationMode
}

func (s Service) ModeForTurnStart(sessionKey, threadID string) *conversation.SessionCollaborationMode {
	if strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(threadID) == "" {
		return nil
	}
	sess := s.Repository.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return nil
	}
	return s.Settings.CaptureMode(sess)
}

type Conversations interface {
	StartWorkspaceThread(string, *conversation.Session, *workspace.Workspace) (*conversation.ThreadBinding, error)
}
type Workspaces interface {
	Get(string) *workspace.Workspace
	DefaultID() string
}
type Queue interface{ StartNextSubmission(string) error }
type Service struct {
	Forms         *interactionapp.FormService
	Delivery      *interactionapp.DeliveryService
	Repository    Repository
	Settings      Settings
	Conversations Conversations
	Workspaces    Workspaces
	Queue         Queue
}
type ConfigureResult struct {
	Mode    *conversation.SessionCollaborationMode
	Enabled bool
}

func (s Service) Configure(key, option string) (ConfigureResult, error) {
	sess := s.Repository.Session(key)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return ConfigureResult{}, fmt.Errorf("当前没有活动线程，无法配置 plan mode")
	}
	enabled := option == "on" || (option == "" && (sess.ActiveThreadCollaborationMode == nil || !strings.EqualFold(sess.ActiveThreadCollaborationMode.Mode, "plan")))
	mode, err := s.Settings.Resolve(sess, enabled)
	if err != nil {
		return ConfigureResult{}, err
	}
	changed := false
	_, err = s.Repository.UpdateSession(key, func(current *conversation.Session) {
		if current != nil && current.ActiveThreadID == sess.ActiveThreadID {
			current.ActiveThreadCollaborationMode = mode
			changed = true
		}
	})
	if err == nil && !changed {
		err = fmt.Errorf("thread 已变化，请重试")
	}
	return ConfigureResult{Mode: mode, Enabled: enabled}, err
}
func (s Service) Clear(key string) (bool, error) {
	sess := s.Repository.Session(key)
	if sess == nil {
		return false, nil
	}
	mode, err := s.Settings.Resolve(sess, false)
	if err != nil {
		return false, err
	}
	changed := false
	_, err = s.Repository.UpdateSession(key, func(current *conversation.Session) {
		if current != nil && current.ActiveThreadID == sess.ActiveThreadID {
			current.ActiveThreadCollaborationMode = mode
			changed = true
		}
	})
	if err == nil && !changed {
		err = fmt.Errorf("thread 已变化，请重试")
	}
	return err == nil, err
}

type ImplementationResult struct {
	Submission *submission.Submission
	ThreadID   string
}

func (s Service) Implement(ctx context.Context, pending *interaction.PendingRequest, fresh bool) (ImplementationResult, error) {
	if pending == nil {
		return ImplementationResult{}, fmt.Errorf("plan confirmation unavailable")
	}
	current, err := s.Authorize(pending.ID, pending.OwnerUserID)
	if err != nil {
		return ImplementationResult{}, err
	}
	pending = current
	planMarkdown, err := confirmationMarkdown(current)
	if err != nil {
		return ImplementationResult{}, err
	}
	sess := s.Repository.Session(pending.SessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(pending.ThreadID) {
		return ImplementationResult{}, fmt.Errorf("请求已过期")
	}
	if conversation.HasActiveWork(sess) || len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		return ImplementationResult{}, fmt.Errorf("当前还有其他任务在处理，请先完成它们")
	}
	if err := s.Forms.SaveDraft(pending.ID, nil, "processing", 0, ""); err != nil {
		return ImplementationResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.Forms.SaveDraft(pending.ID, nil, "pending", 0, "")
		}
	}()
	prompt := "Implement the plan."
	if fresh {
		id := textutil.FirstNonEmpty(sess.ActiveThreadWorkspaceID, sess.WorkspaceID, s.Workspaces.DefaultID())
		ws := s.Workspaces.Get(id)
		if ws == nil {
			return ImplementationResult{}, fmt.Errorf("workspace %q not found", id)
		}
		if _, err := s.Conversations.StartWorkspaceThread(pending.SessionKey, sess, ws); err != nil {
			return ImplementationResult{}, err
		}
		prompt = FreshPrompt(planMarkdown)
	}
	cleared, err := s.Clear(pending.SessionKey)
	if err != nil {
		return ImplementationResult{}, err
	}
	if !cleared {
		return ImplementationResult{}, fmt.Errorf("请求已过期")
	}
	sub, err := s.CreateSubmission(pending, prompt)
	if err != nil {
		return ImplementationResult{}, err
	}
	// Queue admission commits the action, even if later delivery fails.
	committed = true
	if err := s.Forms.SaveDraft(pending.ID, nil, "resolved", 0, ""); err != nil {
		return ImplementationResult{}, err
	}
	if err := s.Queue.StartNextSubmission(pending.SessionKey); err != nil {
		return ImplementationResult{}, err
	}
	started := s.Repository.Submission(sub.ID)
	if started == nil {
		started = sub
	}
	return ImplementationResult{Submission: started, ThreadID: started.ThreadID}, nil
}
func (s Service) Stay(id string) error {
	req := s.Repository.Pending(id)
	if req == nil || req.Status != "pending" {
		return fmt.Errorf("请求已过期")
	}
	return s.Forms.SaveDraft(id, nil, "resolved", 0, "")
}
func (s Service) CreateSubmission(pending *interaction.PendingRequest, inputText string) (*submission.Submission, error) {
	if pending == nil {
		return nil, fmt.Errorf("plan confirmation unavailable")
	}
	sess := s.Repository.Session(pending.SessionKey)
	if sess == nil {
		return nil, fmt.Errorf("session not found")
	}
	anchor := textutil.FirstNonEmpty(pending.FeishuMsgID, sess.RootMessageID)
	sub := &submission.Submission{SessionKey: strings.TrimSpace(pending.SessionKey), WorkspaceID: textutil.FirstNonEmpty(sess.ActiveThreadWorkspaceID, sess.WorkspaceID, s.Workspaces.DefaultID()), ThreadID: strings.TrimSpace(sess.ActiveThreadID), UserID: textutil.FirstNonEmpty(pending.OwnerUserID, sess.OwnerUserID), ChatID: strings.TrimSpace(sess.ChatID), TriggerMessageID: anchor, InputText: strings.TrimSpace(inputText), Status: submission.SubmissionStatusQueued.String()}
	if anchor != "" {
		sub.SourceMessageIDs = []string{anchor}
	}
	if root := textutil.FirstNonEmpty(sess.RootMessageID, pending.FeishuMsgID); root != "" {
		sub.SourceRootMessageIDs = []string{root}
	}
	id, err := s.Repository.CreateSubmission(sub)
	if err != nil {
		return nil, err
	}
	sub.ID = id
	if err := s.Repository.QueueSubmission(pending.SessionKey, id); err != nil {
		return nil, err
	}
	return sub, nil
}
func FreshPrompt(planMarkdown string) string {
	planMarkdown = strings.TrimSpace(planMarkdown)
	intro := "A previous agent produced the plan below to accomplish the user's task. Implement the plan in a fresh context. Treat the plan as the source of user intent, re-read files as needed, and carry the work through implementation and verification."
	if planMarkdown == "" {
		return intro
	}
	return intro + "\n\n" + planMarkdown
}
