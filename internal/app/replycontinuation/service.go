// Package replycontinuation provides reply continuation, message link tracking,
// and steer-inbound-reply logic extracted from the app package.
package replycontinuation

import (
	"fmt"
	"sort"
	"strings"

	"feidex/internal/app/appcore"
	"feidex/internal/app/submission"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// App is the narrow interface that the reply continuation service uses to
// access host-app dependencies. *App satisfies this via its accessor methods.
type App interface {
	appcore.AppConfig

	// DefaultWorkspaceID returns the default workspace ID.
	DefaultWorkspaceID() string
}

// TrySteerFunc is called to attempt steering a reply into an existing
// conversation thread via the active backend.
type TrySteerFunc func(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error)

// StartSubmissionFunc starts a Claude submission for a given session.
type StartSubmissionFunc func(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error

// ResolveInboundAttachmentsFunc downloads and resolves attachments from an
// inbound message.
type ResolveInboundAttachmentsFunc func(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]state.SubmissionAttachment, error)

// Service manages reply continuation, message link tracking, and
// inbound-reply steering logic.
type Service struct {
	App App

	// TrySteer attempts to steer a reply into an active conversation thread.
	TrySteer TrySteerFunc

	// StartSubmission starts a Claude submission for a session.
	StartSubmission StartSubmissionFunc

	// StartSteerSubmission starts a steer submission that sends a message
	// into the current conversation without creating a separate CLI turn.
	StartSteerSubmission StartSubmissionFunc

	// ResolveInboundAttachments resolves attachments from inbound messages.
	ResolveInboundAttachments ResolveInboundAttachmentsFunc

	// GetSession retrieves a session by key from the store.
	GetSession func(key string) *state.Session

	// SaveSession persists a session to the store.
	SaveSession func(sess *state.Session) error

	// GetMessageLink retrieves a message link by ID with frontend scoping.
	GetMessageLink func(messageID string) *state.MessageLink

	// SaveMessageLink persists a message link to the store.
	SaveMessageLink func(link *state.MessageLink) error

	// CreateSubmission creates a new submission in the store.
	CreateSubmission func(sub *state.Submission) (string, error)

	// HasInFlightSubmission returns true if the session has an in-flight submission.
	HasInFlightSubmission func(sess *state.Session) bool
}

// NewService creates a new reply continuation Service.
func NewService(app App) *Service {
	return &Service{App: app}
}

// ReplyRootTurnLink returns the MessageLink for the root message of a reply
// chain, but only if it matches the current backend. Returns nil if the
// message is not a reply or if no matching link exists.
func (s *Service) ReplyRootTurnLink(msg *feishu.InboundMessage) *state.MessageLink {
	if s == nil || s.App == nil || s.App.Store() == nil || msg == nil {
		return nil
	}
	if strings.TrimSpace(msg.ParentMessageID) == "" {
		return nil
	}
	root := strings.TrimSpace(msg.RootMessageID)
	if root == "" || root == strings.TrimSpace(msg.MessageID) {
		return nil
	}
	link := s.GetMessageLink(root)
	if !s.MessageLinkMatchesCurrentBackend(link) {
		return nil
	}
	return link
}

// MessageLinkMatchesCurrentBackend returns true if the given message link
// belongs to the currently active backend.
func (s *Service) MessageLinkMatchesCurrentBackend(link *state.MessageLink) bool {
	if s == nil || s.App == nil || link == nil {
		return false
	}
	currentBackend := appcore.ConfiguredBackend(s.App)
	linkBackend := appcore.NormalizeRuntimeBackend(link.Backend)
	switch {
	case currentBackend == "":
		return linkBackend == ""
	case linkBackend != "":
		return linkBackend == currentBackend
	}
	if strings.TrimSpace(link.SessionKey) == "" || strings.TrimSpace(link.ThreadID) == "" {
		return false
	}
	sess := s.GetSession(link.SessionKey)
	if sess == nil {
		return false
	}
	return strings.TrimSpace(sess.ActiveThreadID) == strings.TrimSpace(link.ThreadID)
}

// SessionKeyForInboundMessage returns the session key to use for an inbound
// reply message. If the message link provides a session key, that is used;
// otherwise a new session key is derived from the message.
func (s *Service) SessionKeyForInboundMessage(msg *feishu.InboundMessage, link *state.MessageLink) string {
	if link != nil && strings.TrimSpace(link.SessionKey) != "" {
		return strings.TrimSpace(link.SessionKey)
	}
	return appcore.MakeSessionKey(s.App, msg)
}

// PendingInputSessionKey returns a bucket session key for pending-input
// staging, scoped to the current frontend.
func (s *Service) PendingInputSessionKey(msg *feishu.InboundMessage) string {
	if msg == nil {
		return ""
	}
	prefix := "feishu:"
	if s.App != nil && strings.TrimSpace(s.App.FrontendID()) != "" {
		prefix += "frontend:" + strings.TrimSpace(s.App.FrontendID()) + ":"
	}
	return prefix + "chat:" + strings.TrimSpace(msg.ChatID) + ":pending:" + strings.TrimSpace(msg.UserID)
}

// CollectPendingStagedImages collects staged images from the given session
// keys, deduplicating and sorting by creation time.
func (s *Service) CollectPendingStagedImages(targetSessionKey, bucketSessionKey string) []state.SessionStagedImage {
	images := []state.SessionStagedImage{}
	seen := map[string]struct{}{}
	for _, key := range []string{strings.TrimSpace(bucketSessionKey), strings.TrimSpace(targetSessionKey)} {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		sess := s.GetSession(key)
		if sess == nil {
			continue
		}
		images = append(images, sess.StagedImages...)
	}
	sort.SliceStable(images, func(i, j int) bool {
		if images[i].CreatedAt == images[j].CreatedAt {
			return images[i].SourceMessageID < images[j].SourceMessageID
		}
		return images[i].CreatedAt < images[j].CreatedAt
	})
	return images
}

// ClearPendingStagedImages clears staged images from the given session keys.
func (s *Service) ClearPendingStagedImages(targetSessionKey, bucketSessionKey string) error {
	seen := map[string]struct{}{}
	for _, key := range []string{strings.TrimSpace(bucketSessionKey), strings.TrimSpace(targetSessionKey)} {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		sess := s.GetSession(key)
		if sess == nil || len(sess.StagedImages) == 0 {
			continue
		}
		sess.StagedImages = nil
		if !s.HasInFlightSubmission(sess) && len(sess.Queue) == 0 {
			sess.Status = state.SessionStatusIdle.String()
		}
		if err := s.SaveSession(sess); err != nil {
			return err
		}
	}
	return nil
}

// TrySteerInboundReply attempts to steer an inbound reply message into an
// existing conversation thread via the active backend. Returns true if the
// reply was successfully steered.
func (s *Service) TrySteerInboundReply(msg *feishu.InboundMessage, link *state.MessageLink) (bool, error) {
	if s == nil || s.App == nil || msg == nil || link == nil {
		return false, nil
	}
	threadID := strings.TrimSpace(link.ThreadID)
	turnID := strings.TrimSpace(link.TurnID)
	if threadID == "" || turnID == "" {
		return false, nil
	}
	sessionKey := s.SessionKeyForInboundMessage(msg, link)
	sess := s.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{
			Key:           sessionKey,
			WorkspaceID:   s.App.DefaultWorkspaceID(),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        state.SessionStatusIdle.String(),
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = s.App.DefaultWorkspaceID()
	}
	return s.TrySteer(msg, link, sessionKey, sess)
}

// TryClaudeReplyContinuation attempts to continue an active Claude session
// with a reply message. Returns true if the continuation was started.
func (s *Service) TryClaudeReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	if s == nil || s.App == nil || msg == nil || link == nil || sess == nil {
		return false, nil
	}
	if !s.HasInFlightSubmission(sess) {
		return false, nil
	}
	if strings.TrimSpace(sess.ActiveThreadID) == "" {
		return false, nil
	}
	sub, err := s.BuildClaudeContinuationSubmissionFromMessage(msg, sessionKey, sess, true)
	if err != nil {
		return false, err
	}
	if sub == nil {
		return false, nil
	}
	if err := s.StartClaudeContinuationSubmission(sessionKey, sub, false); err != nil {
		return false, err
	}
	return true, nil
}

// ContinueClaudeSessionWithText continues an active Claude session with the
// given text input.
func (s *Service) ContinueClaudeSessionWithText(sessionKey, text string) error {
	if s == nil || s.App == nil {
		return fmt.Errorf("app not initialized")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	sess := s.GetSession(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveTurnID) == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	workspaceID := appcore.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), s.App.DefaultWorkspaceID())
	sub := &state.Submission{
		SessionKey:  strings.TrimSpace(sessionKey),
		WorkspaceID: workspaceID,
		UserID:      strings.TrimSpace(sess.OwnerUserID),
		ChatID:      strings.TrimSpace(sess.ChatID),
		InputText:   text,
		Status:      state.SubmissionStatusQueued.String(),
	}
	if rootMessageID := strings.TrimSpace(sess.RootMessageID); rootMessageID != "" {
		sub.SourceRootMessageIDs = []string{rootMessageID}
	}
	id, err := s.CreateSubmission(sub)
	if err != nil {
		return err
	}
	sub.ID = id
	return s.StartClaudeContinuationSubmission(sessionKey, sub, false)
}

// StagedImageAttachments converts staged images to submission attachments,
// delegating to the submission package.
func StagedImageAttachments(images []state.SessionStagedImage) []state.SubmissionAttachment {
	return submission.StagedImageAttachments(images)
}

// StagedImageSourceMessageIDs returns the unique source message IDs from
// staged images, delegating to the submission package.
func StagedImageSourceMessageIDs(images []state.SessionStagedImage) []string {
	return submission.StagedImageSourceMessageIDs(images)
}

// StagedImageRootMessageIDs returns the unique root message IDs from staged
// images, falling back to source message IDs when root is empty. Delegates
// to the submission package.
func StagedImageRootMessageIDs(images []state.SessionStagedImage) []string {
	return submission.StagedImageRootMessageIDs(images)
}

// BuildClaudeContinuationSubmissionFromMessage builds a submission for
// continuing a Claude session from an inbound reply message.
func (s *Service) BuildClaudeContinuationSubmissionFromMessage(msg *feishu.InboundMessage, sessionKey string, sess *state.Session, bindOnlyCurrentRoot bool) (*state.Submission, error) {
	if s == nil || s.App == nil || msg == nil || sess == nil {
		return nil, nil
	}
	workspaceID := appcore.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), s.App.DefaultWorkspaceID())
	bucketSessionKey := s.PendingInputSessionKey(msg)
	inboundAttachments, err := s.ResolveInboundAttachments(msg, workspaceID, sessionKey)
	if err != nil {
		return nil, err
	}
	stagedImages := s.CollectPendingStagedImages(sessionKey, bucketSessionKey)
	sourceMessageIDs := appcore.UniqueStrings(append([]string{msg.MessageID}, StagedImageSourceMessageIDs(stagedImages)...))
	currentRootMessageID := appcore.FirstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID))
	sourceRootMessageIDs := []string{currentRootMessageID}
	if !bindOnlyCurrentRoot {
		sourceRootMessageIDs = appcore.UniqueStrings(append(sourceRootMessageIDs, StagedImageRootMessageIDs(stagedImages)...))
	}
	sub := &state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          workspaceID,
		UserID:               msg.UserID,
		ChatID:               msg.ChatID,
		TriggerMessageID:     msg.MessageID,
		SourceMessageIDs:     sourceMessageIDs,
		SourceRootMessageIDs: sourceRootMessageIDs,
		InputText:            msg.Text,
		Attachments:          append(StagedImageAttachments(stagedImages), inboundAttachments...),
		Status:               state.SubmissionStatusQueued.String(),
	}
	if strings.TrimSpace(sub.InputText) == "" && len(sub.Attachments) == 0 {
		return nil, nil
	}
	id, err := s.CreateSubmission(sub)
	if err != nil {
		return nil, err
	}
	sub.ID = id
	if len(stagedImages) > 0 {
		if err := s.ClearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
			return nil, err
		}
	}
	return sub, nil
}

// StartClaudeContinuationSubmission starts a Claude submission for a
// continuation, looking up the workspace configuration.
func (s *Service) StartClaudeContinuationSubmission(sessionKey string, sub *state.Submission, notifyFailure bool) error {
	if s == nil || s.App == nil || sub == nil {
		return nil
	}
	sess := s.GetSession(sessionKey)
	if sess == nil {
		return fmt.Errorf("session %q missing", sessionKey)
	}
	ws := config.FindWorkspace(s.App.Config(), sub.WorkspaceID)
	if ws == nil {
		return fmt.Errorf("workspace %q not found", sub.WorkspaceID)
	}
	if s.StartSteerSubmission != nil {
		return s.StartSteerSubmission(sessionKey, sess, sub, ws, notifyFailure)
	}
	return s.StartSubmission(sessionKey, sess, sub, ws, notifyFailure)
}

// SourceMessageIDsForSubmission returns the unique source message IDs for a
// submission, delegating to the submission package.
func SourceMessageIDsForSubmission(sub *state.Submission) []string {
	return submission.SourceMessageIDs(sub)
}

// RecordSubmissionSourceLinks records message links for all source messages
// of a submission and root-turn bindings for all source root messages.
func (s *Service) RecordSubmissionSourceLinks(sub *state.Submission) {
	if s == nil || s.App == nil || s.App.Store() == nil || sub == nil {
		return
	}
	sourceMessageIDs := SourceMessageIDsForSubmission(sub)
	if len(sub.SourceRootMessageIDs) > 1 {
		for _, messageID := range sourceMessageIDs {
			s.RecordTurnMessageLink(messageID, sub.SessionKey, sub.ThreadID, sub.TurnID)
		}
	} else if strings.TrimSpace(sub.TriggerMessageID) != "" {
		s.RecordTurnMessageLink(sub.TriggerMessageID, sub.SessionKey, sub.ThreadID, sub.TurnID)
	} else {
		for _, messageID := range sourceMessageIDs {
			s.RecordTurnMessageLink(messageID, sub.SessionKey, sub.ThreadID, sub.TurnID)
		}
	}
	for _, rootID := range sub.SourceRootMessageIDs {
		s.RecordRootTurnBinding(rootID, sub.SessionKey, sub.ThreadID, sub.TurnID)
	}
}

// RecordRootTurnBinding records a message link binding a root message to a
// session/thread/turn.
func (s *Service) RecordRootTurnBinding(rootMessageID, sessionKey, threadID, turnID string) {
	if s == nil || s.App == nil || s.App.Store() == nil || strings.TrimSpace(rootMessageID) == "" {
		return
	}
	_ = s.SaveMessageLink(&state.MessageLink{
		MessageID:  strings.TrimSpace(rootMessageID),
		SessionKey: strings.TrimSpace(sessionKey),
		ThreadID:   strings.TrimSpace(threadID),
		TurnID:     strings.TrimSpace(turnID),
	})
}

// RecordTurnMessageLink records a message link binding a message to a
// session/thread/turn.
func (s *Service) RecordTurnMessageLink(messageID, sessionKey, threadID, turnID string) {
	if s == nil || s.App == nil || s.App.Store() == nil || strings.TrimSpace(messageID) == "" {
		return
	}
	_ = s.SaveMessageLink(&state.MessageLink{
		MessageID:  strings.TrimSpace(messageID),
		SessionKey: strings.TrimSpace(sessionKey),
		ThreadID:   strings.TrimSpace(threadID),
		TurnID:     strings.TrimSpace(turnID),
	})
}
