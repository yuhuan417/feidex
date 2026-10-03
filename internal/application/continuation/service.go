// Package replycontinuation provides reply continuation, message link tracking,
// and steer-inbound-reply logic extracted from the app package.
package continuation

import (
	"feidex/internal/application"
	"feidex/internal/application/submission"
	"feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/domain/workspace"
	"feidex/internal/textutil"
	"fmt"
	"sort"
	"strings"
)

// TrySteerFunc is called to attempt steering a reply into an existing
// conversation thread via the active backend.
type TrySteerFunc func(msg *application.InboundMessage, link *conversation.MessageLink, sessionKey string, sess *conversation.Session) (bool, error)

// StartSubmissionFunc starts a Claude submission for a given session.
type StartSubmissionFunc func(sessionKey string, sess *conversation.Session, sub *domainsubmission.Submission, ws *workspace.Workspace, notifyFailure bool) error

// ResolveInboundAttachmentsFunc downloads and resolves attachments from an
// inbound message.
type ResolveInboundAttachmentsFunc func(msg *application.InboundMessage, workspaceID, sessionKey string) ([]domainsubmission.SubmissionAttachment, error)

// Dependencies are the consumer-owned ports required by reply continuation.
// Keeping them in one explicit carrier prevents the service from becoming a
// second host registry while preserving narrow function-level boundaries.
type Dependencies struct {
	Backend            func() string
	FrontendID         string
	DefaultWorkspaceID func() string
	Workspace          func(string) *workspace.Workspace
	MakeSessionKey     func(*application.InboundMessage) string

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
	GetSession func(key string) *conversation.Session

	// SaveSession persists a session to the store.
	SaveSession func(sess *conversation.Session) error

	// GetMessageLink retrieves a message link by ID with frontend scoping.
	GetMessageLink func(messageID string) *conversation.MessageLink

	// SaveMessageLink persists a message link to the store.
	SaveMessageLink func(link *conversation.MessageLink) error

	// CreateSubmission creates a new submission in the store.
	CreateSubmission func(sub *domainsubmission.Submission) (string, error)

	// HasInFlightSubmission returns true if the session has an in-flight submission.
	HasInFlightSubmission func(sess *conversation.Session) bool
}

// Service manages reply continuation, message link tracking, and
// inbound-reply steering logic.
type Service struct{ Deps Dependencies }

// ReplyRootTurnLink returns the MessageLink for the root message of a reply
// chain, but only if it matches the current backend. Returns nil if the
// message is not a reply or if no matching link exists.
func (s *Service) ReplyRootTurnLink(msg *application.InboundMessage) *conversation.MessageLink {
	if s == nil || msg == nil {
		return nil
	}
	if strings.TrimSpace(msg.ParentMessageID) == "" {
		return nil
	}
	root := strings.TrimSpace(msg.RootMessageID)
	if root == "" || root == strings.TrimSpace(msg.MessageID) {
		return nil
	}
	link := s.Deps.GetMessageLink(root)
	if !s.MessageLinkMatchesCurrentBackend(link) {
		return nil
	}
	return link
}

// MessageLinkMatchesCurrentBackend returns true if the given message link
// belongs to the currently active backend.
func (s *Service) MessageLinkMatchesCurrentBackend(link *conversation.MessageLink) bool {
	if s == nil || link == nil {
		return false
	}
	currentBackend := s.Deps.Backend()
	linkBackend := backend.NormalizeBackend(link.Backend)
	switch {
	case currentBackend == "":
		return linkBackend == ""
	case linkBackend != "":
		return linkBackend == currentBackend
	}
	if strings.TrimSpace(link.SessionKey) == "" || strings.TrimSpace(link.ThreadID) == "" {
		return false
	}
	sess := s.Deps.GetSession(link.SessionKey)
	if sess == nil {
		return false
	}
	return strings.TrimSpace(sess.ActiveThreadID) == strings.TrimSpace(link.ThreadID)
}

// SessionKeyForInboundMessage returns the session key to use for an inbound
// reply message. If the message link provides a session key, that is used;
// otherwise a new session key is derived from the message.
func (s *Service) SessionKeyForInboundMessage(msg *application.InboundMessage, link *conversation.MessageLink) string {
	if link != nil && strings.TrimSpace(link.SessionKey) != "" {
		return strings.TrimSpace(link.SessionKey)
	}
	return s.Deps.MakeSessionKey(msg)
}

// PendingInputSessionKey returns a bucket session key for pending-input
// staging, scoped to the current frontend.
func (s *Service) PendingInputSessionKey(msg *application.InboundMessage) string {
	if msg == nil {
		return ""
	}
	prefix := "feishu:"
	if strings.TrimSpace(s.Deps.FrontendID) != "" {
		prefix += "frontend:" + strings.TrimSpace(s.Deps.FrontendID) + ":"
	}
	return prefix + "chat:" + strings.TrimSpace(msg.ChatID) + ":pending:" + strings.TrimSpace(msg.UserID)
}

// CollectPendingStagedImages collects staged images from the given session
// keys, deduplicating and sorting by creation time.
func (s *Service) CollectPendingStagedImages(targetSessionKey, bucketSessionKey string) []conversation.SessionStagedImage {
	images := []conversation.SessionStagedImage{}
	seen := map[string]struct{}{}
	for _, key := range []string{strings.TrimSpace(bucketSessionKey), strings.TrimSpace(targetSessionKey)} {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		sess := s.Deps.GetSession(key)
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
		sess := s.Deps.GetSession(key)
		if sess == nil || len(sess.StagedImages) == 0 {
			continue
		}
		sess.StagedImages = nil
		if !s.Deps.HasInFlightSubmission(sess) && len(sess.Queue) == 0 {
			sess.Status = conversation.SessionStatusIdle.String()
		}
		if err := s.Deps.SaveSession(sess); err != nil {
			return err
		}
	}
	return nil
}

// TrySteerInboundReply attempts to steer an inbound reply message into an
// existing conversation thread via the active backend. Returns true if the
// reply was successfully steered.
func (s *Service) TrySteerInboundReply(msg *application.InboundMessage, link *conversation.MessageLink) (bool, error) {
	if s == nil || msg == nil || link == nil {
		return false, nil
	}
	threadID := strings.TrimSpace(link.ThreadID)
	turnID := strings.TrimSpace(link.TurnID)
	if threadID == "" || turnID == "" {
		return false, nil
	}
	sessionKey := s.SessionKeyForInboundMessage(msg, link)
	sess := s.Deps.GetSession(sessionKey)
	if sess == nil {
		sess = &conversation.Session{
			Key:           sessionKey,
			WorkspaceID:   s.Deps.DefaultWorkspaceID(),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        conversation.SessionStatusIdle.String(),
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = s.Deps.DefaultWorkspaceID()
	}
	return s.Deps.TrySteer(msg, link, sessionKey, sess)
}

// TryClaudeReplyContinuation attempts to continue an active Claude session
// with a reply message. Returns true if the continuation was started.
func (s *Service) TryClaudeReplyContinuation(msg *application.InboundMessage, link *conversation.MessageLink, sessionKey string, sess *conversation.Session) (bool, error) {
	if s == nil || msg == nil || link == nil || sess == nil {
		return false, nil
	}
	if !s.Deps.HasInFlightSubmission(sess) {
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
	if s == nil {
		return fmt.Errorf("app not initialized")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	sess := s.Deps.GetSession(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveTurnID) == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	workspaceID := textutil.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), s.Deps.DefaultWorkspaceID())
	sub := &domainsubmission.Submission{
		SessionKey:  strings.TrimSpace(sessionKey),
		WorkspaceID: workspaceID,
		UserID:      strings.TrimSpace(sess.OwnerUserID),
		ChatID:      strings.TrimSpace(sess.ChatID),
		InputText:   text,
		Status:      domainsubmission.SubmissionStatusQueued.String(),
	}
	if rootMessageID := strings.TrimSpace(sess.RootMessageID); rootMessageID != "" {
		sub.SourceRootMessageIDs = []string{rootMessageID}
	}
	id, err := s.Deps.CreateSubmission(sub)
	if err != nil {
		return err
	}
	sub.ID = id
	return s.StartClaudeContinuationSubmission(sessionKey, sub, false)
}

// StagedImageAttachments converts staged images to submission attachments,
// delegating to the submission package.
func StagedImageAttachments(images []conversation.SessionStagedImage) []domainsubmission.SubmissionAttachment {
	return submission.StagedImageAttachments(images)
}

// StagedImageSourceMessageIDs returns the unique source message IDs from
// staged images, delegating to the submission package.
func StagedImageSourceMessageIDs(images []conversation.SessionStagedImage) []string {
	return submission.StagedImageSourceMessageIDs(images)
}

// StagedImageRootMessageIDs returns the unique root message IDs from staged
// images, falling back to source message IDs when root is empty. Delegates
// to the submission package.
func StagedImageRootMessageIDs(images []conversation.SessionStagedImage) []string {
	return submission.StagedImageRootMessageIDs(images)
}

// BuildClaudeContinuationSubmissionFromMessage builds a submission for
// continuing a Claude session from an inbound reply message.
func (s *Service) BuildClaudeContinuationSubmissionFromMessage(msg *application.InboundMessage, sessionKey string, sess *conversation.Session, bindOnlyCurrentRoot bool) (*domainsubmission.Submission, error) {
	if s == nil || msg == nil || sess == nil {
		return nil, nil
	}
	workspaceID := textutil.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), s.Deps.DefaultWorkspaceID())
	bucketSessionKey := s.PendingInputSessionKey(msg)
	inboundAttachments, err := s.Deps.ResolveInboundAttachments(msg, workspaceID, sessionKey)
	if err != nil {
		return nil, err
	}
	stagedImages := s.CollectPendingStagedImages(sessionKey, bucketSessionKey)
	sourceMessageIDs := submission.UniqueStrings(append([]string{msg.MessageID}, StagedImageSourceMessageIDs(stagedImages)...))
	currentRootMessageID := textutil.FirstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID))
	sourceRootMessageIDs := []string{currentRootMessageID}
	if !bindOnlyCurrentRoot {
		sourceRootMessageIDs = submission.UniqueStrings(append(sourceRootMessageIDs, StagedImageRootMessageIDs(stagedImages)...))
	}
	sub := &domainsubmission.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          workspaceID,
		UserID:               msg.UserID,
		ChatID:               msg.ChatID,
		TriggerMessageID:     msg.MessageID,
		SourceMessageIDs:     sourceMessageIDs,
		SourceRootMessageIDs: sourceRootMessageIDs,
		InputText:            msg.Text,
		Attachments:          append(StagedImageAttachments(stagedImages), inboundAttachments...),
		Status:               domainsubmission.SubmissionStatusQueued.String(),
	}
	if strings.TrimSpace(sub.InputText) == "" && len(sub.Attachments) == 0 {
		return nil, nil
	}
	id, err := s.Deps.CreateSubmission(sub)
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
func (s *Service) StartClaudeContinuationSubmission(sessionKey string, sub *domainsubmission.Submission, notifyFailure bool) error {
	if s == nil || sub == nil {
		return nil
	}
	sess := s.Deps.GetSession(sessionKey)
	if sess == nil {
		return fmt.Errorf("session %q missing", sessionKey)
	}
	ws := s.Deps.Workspace(sub.WorkspaceID)
	if ws == nil {
		return fmt.Errorf("workspace %q not found", sub.WorkspaceID)
	}
	if s.Deps.StartSteerSubmission != nil {
		return s.Deps.StartSteerSubmission(sessionKey, sess, sub, ws, notifyFailure)
	}
	return s.Deps.StartSubmission(sessionKey, sess, sub, ws, notifyFailure)
}

// SourceMessageIDsForSubmission returns the unique source message IDs for a
// submission, delegating to the submission package.
func SourceMessageIDsForSubmission(sub *domainsubmission.Submission) []string {
	return submission.SourceMessageIDs(sub)
}

// RecordSubmissionSourceLinks records message links for all source messages
// of a submission and root-turn bindings for all source root messages.
func (s *Service) RecordSubmissionSourceLinks(sub *domainsubmission.Submission) {
	if s == nil || sub == nil {
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
	if s == nil || strings.TrimSpace(rootMessageID) == "" {
		return
	}
	_ = s.Deps.SaveMessageLink(&conversation.MessageLink{
		MessageID:  strings.TrimSpace(rootMessageID),
		SessionKey: strings.TrimSpace(sessionKey),
		ThreadID:   strings.TrimSpace(threadID),
		TurnID:     strings.TrimSpace(turnID),
	})
}

// RecordTurnMessageLink records a message link binding a message to a
// session/thread/turn.
func (s *Service) RecordTurnMessageLink(messageID, sessionKey, threadID, turnID string) {
	if s == nil || strings.TrimSpace(messageID) == "" {
		return
	}
	_ = s.Deps.SaveMessageLink(&conversation.MessageLink{
		MessageID:  strings.TrimSpace(messageID),
		SessionKey: strings.TrimSpace(sessionKey),
		ThreadID:   strings.TrimSpace(threadID),
		TurnID:     strings.TrimSpace(turnID),
	})
}
