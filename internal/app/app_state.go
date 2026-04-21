package app

import (
	"strings"

	"feidex/internal/state"
)

// appStateFacade centralizes app-level access to state.Store so lifecycle
// code can depend on a narrower surface before state is split further.
type appStateFacade struct {
	store          *state.Store
	frontendID     string
	backend        string
	legacyFallback bool
}

func (a *App) appState() *appStateFacade {
	return &appStateFacade{
		store:          a.store,
		frontendID:     strings.TrimSpace(a.frontendID),
		backend:        normalizeRuntimeBackend(a.configuredBackend()),
		legacyFallback: a.allowLegacyFrontendFallback(),
	}
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
	cp := stateCloneSession(sess)
	if cp == nil {
		return nil
	}
	if s.backend != "" {
		sessionStoreBackendThread(cp, s.backend)
	}
	return s.store.UpsertSession(cp)
}

func (s *appStateFacade) updateSession(key string, mutate func(*state.Session)) (*state.Session, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	return s.store.UpdateSession(strings.TrimSpace(key), func(sess *state.Session) {
		if mutate != nil {
			mutate(sess)
		}
		if s.backend != "" {
			sessionStoreBackendThread(sess, s.backend)
		}
	})
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
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if req := s.store.PendingByScopedID(s.frontendID, id); req != nil {
		return req
	}
	if s.legacyFallback && s.frontendID != "" {
		return s.store.PendingByID(id)
	}
	return nil
}

func (s *appStateFacade) savePending(req *state.PendingRequest) error {
	if s == nil || s.store == nil || req == nil {
		return nil
	}
	cp := *req
	if strings.TrimSpace(cp.FrontendID) == "" {
		cp.FrontendID = s.frontendID
	}
	if strings.TrimSpace(cp.Backend) == "" {
		cp.Backend = s.backend
	}
	return s.store.UpsertPending(&cp)
}

func (s *appStateFacade) pendingRequests() []*state.PendingRequest {
	if s == nil || s.store == nil {
		return nil
	}
	all := s.store.AllPendingRequests()
	out := make([]*state.PendingRequest, 0, len(all))
	for _, req := range all {
		if req == nil || !s.matchesFrontend(req.FrontendID) {
			continue
		}
		out = append(out, req)
	}
	return out
}

func (s *appStateFacade) updatePending(id string, mutate func(*state.PendingRequest)) error {
	if s == nil || s.store == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	err := s.store.UpdateScopedPending(s.frontendID, id, mutate)
	if err == nil || !s.legacyFallback || s.frontendID == "" {
		return err
	}
	return s.store.UpdatePending(id, mutate)
}

func (s *appStateFacade) resolvePending(id string) *state.PendingRequest {
	_ = s.updatePending(id, func(req *state.PendingRequest) { req.Status = "resolved" })
	return s.pending(id)
}

func (s *appStateFacade) deletePendingRequests(match func(*state.PendingRequest) bool) {
	if s == nil || s.store == nil || match == nil {
		return
	}
	s.store.DeletePendingRequests(func(req *state.PendingRequest) bool {
		if req == nil || !s.matchesFrontend(req.FrontendID) {
			return false
		}
		return match(req)
	})
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
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}
	if link := s.store.GetScopedMessageLink(s.frontendID, messageID); link != nil {
		return link
	}
	if s.legacyFallback && s.frontendID != "" {
		return s.store.GetMessageLink(messageID)
	}
	return nil
}

func (s *appStateFacade) saveMessageLink(link *state.MessageLink) error {
	if s == nil || s.store == nil || link == nil {
		return nil
	}
	cp := *link
	if strings.TrimSpace(cp.FrontendID) == "" {
		cp.FrontendID = s.frontendID
	}
	if strings.TrimSpace(cp.Backend) == "" {
		cp.Backend = s.backend
	}
	return s.store.UpsertMessageLink(&cp)
}

func (s *appStateFacade) deleteMessageLinks(match func(*state.MessageLink) bool) {
	if s == nil || s.store == nil || match == nil {
		return
	}
	s.store.DeleteMessageLinks(func(link *state.MessageLink) bool {
		if link == nil || !s.matchesFrontend(link.FrontendID) {
			return false
		}
		return match(link)
	})
}

func (s *appStateFacade) matchesFrontend(frontendID string) bool {
	frontendID = strings.TrimSpace(frontendID)
	if frontendID == strings.TrimSpace(s.frontendID) {
		return true
	}
	return frontendID == "" && s.legacyFallback
}

func stateCloneSession(sess *state.Session) *state.Session {
	if sess == nil {
		return nil
	}
	cp := *sess
	cp.Queue = append([]string(nil), sess.Queue...)
	cp.ActiveOperations = append([]state.SessionActiveOperation(nil), sess.ActiveOperations...)
	cp.StagedImages = append([]state.SessionStagedImage(nil), sess.StagedImages...)
	if len(sess.BackendThreads) > 0 {
		cp.BackendThreads = make(map[string]state.SessionBackendThread, len(sess.BackendThreads))
		for key, value := range sess.BackendThreads {
			cp.BackendThreads[key] = value
		}
	}
	return &cp
}
