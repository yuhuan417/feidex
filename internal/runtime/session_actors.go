package runtime

import (
	"strings"
	"sync"
)

// SessionActors serializes transitions that target the same frontend session.
// Different sessions can continue concurrently. The lock entry is removed
// after the last waiter leaves so transient session keys do not accumulate.
type SessionActors struct {
	mu    sync.Mutex
	locks map[string]*sessionActorLock
}

type sessionActorLock struct {
	mu   sync.Mutex
	refs int
}

func NewSessionActors() *SessionActors {
	return &SessionActors{locks: make(map[string]*sessionActorLock)}
}

// Run executes fn while holding the actor lock for key. Empty keys are treated
// as a shared transport scope, preserving ordering for events without a
// session identifier.
func (a *SessionActors) Run(key string, fn func()) {
	if fn == nil {
		return
	}
	if a == nil {
		fn()
		return
	}
	key = strings.TrimSpace(key)
	a.mu.Lock()
	if a.locks == nil {
		a.locks = make(map[string]*sessionActorLock)
	}
	entry := a.locks[key]
	if entry == nil {
		entry = &sessionActorLock{}
		a.locks[key] = entry
	}
	entry.refs++
	a.mu.Unlock()

	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		a.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(a.locks, key)
		}
		a.mu.Unlock()
	}()
	fn()
}
