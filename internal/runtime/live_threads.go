package runtime

import (
	"strings"
	"sync"
)

// LiveThreads tracks conversations attached to one frontend's current process.
// It is deliberately transient: persisted lineage does not prove a thread is
// attached after restart, recovery or backend switching.
type LiveThreads struct {
	mu      sync.Mutex
	threads map[string]string
}

func NewLiveThreads() *LiveThreads {
	return &LiveThreads{}
}

func (t *LiveThreads) Mark(sessionKey, threadID string) {
	sessionKey, threadID = strings.TrimSpace(sessionKey), strings.TrimSpace(threadID)
	if sessionKey == "" || threadID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.threads == nil {
		t.threads = map[string]string{}
	}
	t.threads[sessionKey] = threadID
}

func (t *LiveThreads) Has(sessionKey, threadID string) bool {
	sessionKey, threadID = strings.TrimSpace(sessionKey), strings.TrimSpace(threadID)
	if sessionKey == "" || threadID == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.threads[sessionKey] == threadID
}

func (t *LiveThreads) Clear(sessionKey string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.threads, strings.TrimSpace(sessionKey))
}
