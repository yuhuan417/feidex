package state

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/submission"
	"fmt"
	"maps"
	"reflect"
	"time"
)

// CommitTerminal replaces detached owner snapshots together. Callers choose
// the transition; storage validates the read set and rolls back failed saves.
func (s *Store) CommitTerminal(expectedSession, nextSession *conversation.Session, expectedSubmission, nextSubmission *submission.Submission, expectedRequests, nextRequests []*interaction.PendingRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedSession == nil || expectedSubmission == nil || nextSession == nil || nextSubmission == nil {
		return fmt.Errorf("incomplete terminal transition")
	}
	if nextSession.Key != expectedSession.Key || nextSubmission.ID != expectedSubmission.ID || nextSubmission.SessionKey != expectedSubmission.SessionKey {
		return fmt.Errorf("terminal identity changed")
	}
	if !reflect.DeepEqual(s.runtime.Sessions[expectedSession.Key], expectedSession) || !reflect.DeepEqual(s.runtime.Submissions[expectedSubmission.ID], expectedSubmission) {
		return fmt.Errorf("terminal owner changed")
	}
	readSet := make(map[string]bool, len(expectedRequests))
	for _, req := range expectedRequests {
		if req == nil || !reflect.DeepEqual(s.runtime.PendingRequests[pendingStoreKey(req.FrontendID, req.ID)], req) {
			return fmt.Errorf("terminal interaction changed")
		}
		readSet[pendingStoreKey(req.FrontendID, req.ID)] = true
	}
	for key, req := range s.runtime.PendingRequests {
		if req != nil && req.SessionKey == expectedSession.Key && !readSet[key] {
			return fmt.Errorf("terminal interaction added")
		}
	}
	for _, req := range nextRequests {
		if req == nil || req.SessionKey != expectedSession.Key || !readSet[pendingStoreKey(req.FrontendID, req.ID)] {
			return fmt.Errorf("foreign terminal interaction")
		}
	}
	oldSessions, oldStored := s.runtime.Sessions, s.data.Sessions
	oldSubmissions, oldRequests := s.runtime.Submissions, s.runtime.PendingRequests
	s.runtime.Sessions, s.data.Sessions = maps.Clone(oldSessions), maps.Clone(oldStored)
	s.runtime.Submissions, s.runtime.PendingRequests = maps.Clone(oldSubmissions), maps.Clone(oldRequests)
	sess := cloneSession(nextSession)
	normalizeSessionValues(sess)
	sess.UpdatedAt = time.Now().Unix()
	s.runtime.Sessions[sess.Key] = sess
	s.syncPersistentSessionLocked(sess)
	sub := cloneSubmission(nextSubmission)
	normalizeSubmissionValues(sub)
	sub.UpdatedAt = time.Now().Unix()
	s.runtime.Submissions[sub.ID] = sub
	for _, req := range nextRequests {
		cp := *req
		normalizePendingRequestValues(&cp)
		s.runtime.PendingRequests[pendingStoreKey(cp.FrontendID, cp.ID)] = &cp
	}
	if err := s.saveLocked(); err != nil {
		s.runtime.Sessions, s.data.Sessions = oldSessions, oldStored
		s.runtime.Submissions, s.runtime.PendingRequests = oldSubmissions, oldRequests
		return err
	}
	return nil
}
