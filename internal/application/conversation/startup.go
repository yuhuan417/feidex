package conversation

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	"fmt"
	"strings"
	"time"
)

type StartupRepository interface {
	Sessions() []*conversation.Session
	SaveSession(*conversation.Session) error
	PendingRequests() []*interaction.PendingRequest
	UpdatePending(string, func(*interaction.PendingRequest)) error
}

// StartupState receives only the current frontend's repository projection.
type StartupState struct {
	Repository         StartupRepository
	DefaultWorkspaceID func() string
}

func (s StartupState) Reset() error {
	for _, sess := range s.Repository.Sessions() {
		if sess == nil {
			continue
		}
		if strings.TrimSpace(sess.WorkspaceID) == "" {
			sess.WorkspaceID = s.DefaultWorkspaceID()
		}
		conversation.ResetActiveOperations(sess)
		sess.Queue, sess.StagedImages = nil, nil
		sess.Status = conversation.SessionStatusIdle.String()
		if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
			conversation.ClearThreadContext(sess)
		}
		if err := s.Repository.SaveSession(sess); err != nil {
			return fmt.Errorf("reset startup session %s: %w", sess.Key, err)
		}
	}
	return s.ExpireInteractions()
}

func (s StartupState) ExpireInteractions() error {
	now := time.Now().Unix()
	for _, req := range s.Repository.PendingRequests() {
		if req == nil {
			continue
		}
		status := strings.TrimSpace(req.Status)
		if status != "pending" && status != "replied" && status != "processing" && status != "cancelling" {
			continue
		}
		if err := s.Repository.UpdatePending(req.ID, func(current *interaction.PendingRequest) {
			current.Status = "expired"
			if current.ExpiresAt == 0 || current.ExpiresAt > now {
				current.ExpiresAt = now
			}
		}); err != nil {
			return fmt.Errorf("expire startup interaction %s: %w", req.ID, err)
		}
	}
	return nil
}
