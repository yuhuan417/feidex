package runtime

import (
	"strings"
	"sync"

	"feidex/internal/domain/submission"
)

// SubmissionStarts serializes startup for queued work within one frontend.
// Concurrent arrivals request one follow-up pass instead of starting twice.
type SubmissionStarts struct {
	mu       sync.Mutex
	sessions map[string]submission.StartState
}

func (g *SubmissionStarts) TryBegin(sessionKey string) bool {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sessions == nil {
		g.sessions = map[string]submission.StartState{}
	}
	next, admitted := g.sessions[key].Begin()
	g.sessions[key] = next
	return admitted
}

func (g *SubmissionStarts) Finish(sessionKey string) bool {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	_, retry := g.sessions[key].Finish()
	delete(g.sessions, key)
	return retry
}
