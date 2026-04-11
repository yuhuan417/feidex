package app

import (
	"strings"

	"feidex/internal/state"
)

// appStateFacade centralizes app-level access to state.Store so lifecycle
// code can depend on a narrower surface before state is split further.
type appStateFacade struct {
	store *state.Store
}

func (a *App) appState() *appStateFacade {
	return &appStateFacade{store: a.store}
}

func (s *appStateFacade) session(key string) *state.Session {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.GetSession(strings.TrimSpace(key))
}

func (s *appStateFacade) sessions() []*state.Session {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.AllSessions()
}

func (s *appStateFacade) saveSession(sess *state.Session) error {
	if s == nil || s.store == nil || sess == nil {
		return nil
	}
	return s.store.UpsertSession(sess)
}

func (s *appStateFacade) createSubmission(sub *state.Submission) (string, error) {
	if s == nil || s.store == nil {
		return "", nil
	}
	return s.store.CreateSubmission(sub)
}

func (s *appStateFacade) deleteSubmission(id string) {
	if s == nil || s.store == nil {
		return
	}
	s.store.DeleteSubmission(strings.TrimSpace(id))
}

func (s *appStateFacade) submission(id string) *state.Submission {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.GetSubmission(strings.TrimSpace(id))
}

func (s *appStateFacade) updateSubmission(id string, mutate func(*state.Submission)) error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.UpdateSubmission(strings.TrimSpace(id), mutate)
}

func (s *appStateFacade) setSubmissionStatus(id, status string) error {
	return s.updateSubmission(id, func(sub *state.Submission) {
		sub.Status = strings.TrimSpace(status)
	})
}

func (s *appStateFacade) setSubmissionStatusCard(id, cardID string) error {
	return s.updateSubmission(id, func(sub *state.Submission) {
		sub.StatusCardID = strings.TrimSpace(cardID)
	})
}

func (s *appStateFacade) markSubmissionRunning(id, threadID, turnID string) error {
	return s.updateSubmission(id, func(sub *state.Submission) {
		sub.ThreadID = strings.TrimSpace(threadID)
		if strings.TrimSpace(turnID) != "" {
			sub.TurnID = strings.TrimSpace(turnID)
		}
		sub.Status = "running"
	})
}

func (s *appStateFacade) finalizeSubmission(id, status string) error {
	return s.updateSubmission(id, func(sub *state.Submission) {
		sub.Status = strings.TrimSpace(status)
		sub.Finalized = true
	})
}

func (s *appStateFacade) queueSubmission(sessionKey, submissionID string) error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.QueueSubmission(strings.TrimSpace(sessionKey), strings.TrimSpace(submissionID))
}

func (s *appStateFacade) dequeueSubmission(sessionKey string) (string, error) {
	if s == nil || s.store == nil {
		return "", nil
	}
	return s.store.DequeueSubmission(strings.TrimSpace(sessionKey))
}

func (s *appStateFacade) pending(id string) *state.PendingRequest {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.PendingByID(strings.TrimSpace(id))
}

func (s *appStateFacade) savePending(req *state.PendingRequest) error {
	if s == nil || s.store == nil || req == nil {
		return nil
	}
	return s.store.UpsertPending(req)
}

func (s *appStateFacade) pendingRequests() []*state.PendingRequest {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.AllPendingRequests()
}

func (s *appStateFacade) updatePending(id string, mutate func(*state.PendingRequest)) error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.UpdatePending(strings.TrimSpace(id), mutate)
}

func (s *appStateFacade) resolvePending(id string) *state.PendingRequest {
	_ = s.updatePending(id, func(req *state.PendingRequest) { req.Status = "resolved" })
	return s.pending(id)
}

func (s *appStateFacade) deletePendingRequests(match func(*state.PendingRequest) bool) {
	if s == nil || s.store == nil {
		return
	}
	s.store.DeletePendingRequests(match)
}

func (s *appStateFacade) nextLocalID(prefix string) (string, error) {
	if s == nil || s.store == nil {
		return "", nil
	}
	return s.store.NextLocalID(strings.TrimSpace(prefix))
}

func (s *appStateFacade) messageLink(messageID string) *state.MessageLink {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.GetMessageLink(strings.TrimSpace(messageID))
}

func (s *appStateFacade) saveMessageLink(link *state.MessageLink) error {
	if s == nil || s.store == nil || link == nil {
		return nil
	}
	return s.store.UpsertMessageLink(link)
}

func (s *appStateFacade) deleteMessageLinks(match func(*state.MessageLink) bool) {
	if s == nil || s.store == nil {
		return
	}
	s.store.DeleteMessageLinks(match)
}
