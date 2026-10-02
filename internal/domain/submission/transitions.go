package submission

import "strings"

// SetStatus records a status chosen by the lifecycle owner. In particular,
// waiting interactions are resumed only after the application receives their
// authoritative resolved event.
func (s *Submission) SetStatus(status string) {
	if s == nil {
		return
	}
	s.Status = NormalizeSubmissionStatus(status).String()
}

// MarkRunning attaches the acknowledged backend turn. An empty turn ID does
// not erase an existing binding, as turn/start and turn/started can race.
func (s *Submission) MarkRunning(threadID, turnID string) {
	if s == nil {
		return
	}
	s.ThreadID = strings.TrimSpace(threadID)
	if turnID = strings.TrimSpace(turnID); turnID != "" {
		s.TurnID = turnID
	}
	s.Status = SubmissionStatusRunning.String()
}

// Finalize records the terminal result selected by the turn lifecycle owner.
// It leaves source anchors, input, and the captured model snapshot intact.
func (s *Submission) Finalize(status string) {
	if s == nil {
		return
	}
	s.SetStatus(status)
	s.Finalized = true
}
