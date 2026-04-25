package app

import (
	"strings"

	"feidex/internal/app/appcore"
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

func appState(a *App) *appStateFacade {
	return &appStateFacade{
		store:          a.store,
		frontendID:     strings.TrimSpace(a.frontendID),
		backend:        appcore.NormalizeRuntimeBackend(appcore.ConfiguredBackend(a)),
		legacyFallback: appcore.AllowLegacyFrontendFallback(a),
	}
}

func (s *appStateFacade) matchesFrontend(frontendID string) bool {
	frontendID = strings.TrimSpace(frontendID)
	if frontendID == strings.TrimSpace(s.frontendID) {
		return true
	}
	return frontendID == "" && s.legacyFallback
}

var stateCloneSession = appcore.StateCloneSession

// Exported wrappers so appStateFacade directly satisfies sub-package
// provider interfaces (e.g. submission.QueueAppStateProvider) without
// needing separate adapter structs.

func (s *appStateFacade) Session(key string) *state.Session                { return s.session(key) }
func (s *appStateFacade) GetSession(key string) *state.Session             { return s.session(key) }
func (s *appStateFacade) Sessions() []*state.Session                       { return s.sessions() }
func (s *appStateFacade) AllSessions() []*state.Session                    { return s.sessions() }
func (s *appStateFacade) SaveSession(sess *state.Session) error            { return s.saveSession(sess) }
func (s *appStateFacade) UpdateSession(key string, mutate func(*state.Session)) (*state.Session, error) {
	return s.updateSession(key, mutate)
}
func (s *appStateFacade) CreateSubmission(sub *state.Submission) (string, error) {
	return s.createSubmission(sub)
}
func (s *appStateFacade) DeleteSubmission(id string)                       { s.deleteSubmission(id) }
func (s *appStateFacade) Submission(id string) *state.Submission           { return s.submission(id) }
func (s *appStateFacade) UpdateSubmission(id string, mutate func(*state.Submission)) error {
	return s.updateSubmission(id, mutate)
}
func (s *appStateFacade) SetSubmissionStatus(id, status string) error {
	return s.setSubmissionStatus(id, status)
}
func (s *appStateFacade) MarkSubmissionRunning(id, threadID, turnID string) error {
	return s.markSubmissionRunning(id, threadID, turnID)
}
func (s *appStateFacade) FinalizeSubmission(id, status string) error {
	return s.finalizeSubmission(id, status)
}
func (s *appStateFacade) QueueSubmission(sessionKey, submissionID string) error {
	return s.queueSubmission(sessionKey, submissionID)
}
func (s *appStateFacade) DequeueSubmission(sessionKey string) (string, error) {
	return s.dequeueSubmission(sessionKey)
}
func (s *appStateFacade) NextLocalID(prefix string) (string, error) { return s.nextLocalID(prefix) }
func (s *appStateFacade) MessageLink(messageID string) *state.MessageLink {
	return s.messageLink(messageID)
}
func (s *appStateFacade) SaveMessageLink(link *state.MessageLink) error {
	return s.saveMessageLink(link)
}
func (s *appStateFacade) QueueFrontendCardNotification(note state.FrontendCardNotification) error {
	return s.queueFrontendCardNotification(note)
}
func (s *appStateFacade) FrontendCardNotifications() []state.FrontendCardNotification {
	return s.frontendCardNotifications()
}
func (s *appStateFacade) DrainFrontendCardNotifications() ([]state.FrontendCardNotification, error) {
	return s.drainFrontendCardNotifications()
}
func (s *appStateFacade) Pending(id string) *state.PendingRequest         { return s.pending(id) }
func (s *appStateFacade) SavePending(req *state.PendingRequest) error     { return s.savePending(req) }
func (s *appStateFacade) PendingRequests() []*state.PendingRequest        { return s.pendingRequests() }
func (s *appStateFacade) UpdatePending(id string, mutate func(*state.PendingRequest)) error {
	return s.updatePending(id, mutate)
}
func (s *appStateFacade) ResolvePending(id string) *state.PendingRequest {
	return s.resolvePending(id)
}
func (s *appStateFacade) DeletePendingRequests(match func(*state.PendingRequest) bool) {
	s.deletePendingRequests(match)
}
func (s *appStateFacade) DeleteMessageLinks(match func(*state.MessageLink) bool) {
	s.deleteMessageLinks(match)
}
