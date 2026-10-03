// Package skill owns frontend-local pending skill storage. Selection policy
// lives in application/skill; this repository only serializes access.
package skill

import (
	domainsubmission "feidex/internal/domain/submission"
	"strings"
	"sync"
)

// Tracker tracks per-session pending skills.
type Tracker struct {
	mu     sync.Mutex
	skills map[string]domainsubmission.SubmissionSkill
}

// NewTracker creates a new Tracker.
func NewTracker() *Tracker {
	return &Tracker{skills: map[string]domainsubmission.SubmissionSkill{}}
}

// SessionPendingSkill returns the pending skill for the given session key.
func (tracker *Tracker) Get(sessionKey string) (domainsubmission.SubmissionSkill, bool) {
	if tracker == nil {
		return domainsubmission.SubmissionSkill{}, false
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	skill, ok := tracker.skills[strings.TrimSpace(sessionKey)]
	if !ok || strings.TrimSpace(skill.Name) == "" || strings.TrimSpace(skill.Path) == "" {
		return domainsubmission.SubmissionSkill{}, false
	}
	return skill, true
}

// SetSessionPendingSkill sets the pending skill for the given session key.
func (tracker *Tracker) Set(sessionKey string, skill domainsubmission.SubmissionSkill) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	if tracker == nil {
		return
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.skills == nil {
		tracker.skills = map[string]domainsubmission.SubmissionSkill{}
	}
	skill.Name = strings.TrimSpace(skill.Name)
	skill.Path = strings.TrimSpace(skill.Path)
	if skill.Name == "" || skill.Path == "" {
		delete(tracker.skills, strings.TrimSpace(sessionKey))
		return
	}
	tracker.skills[strings.TrimSpace(sessionKey)] = skill
}

// ClearSessionPendingSkill clears the pending skill for the given session key.
func (tracker *Tracker) Clear(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	if tracker == nil {
		return
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	delete(tracker.skills, strings.TrimSpace(sessionKey))
}
