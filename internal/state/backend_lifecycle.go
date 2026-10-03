package state

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"time"

	"feidex/internal/domain/conversation"
)

// CommitBackendSessions commits frontend lineage as one snapshot, then writes
// the config file. The hook performs only storage work under the state lock.
func (s *Store) CommitBackendSessions(expected, next []*conversation.Session, persistConfig func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range expected {
		if sess == nil || !reflect.DeepEqual(s.runtime.Sessions[sess.Key], sess) {
			return fmt.Errorf("后端会话状态已变化，请重试")
		}
	}
	oldRuntime, oldStored := s.runtime.Sessions, s.data.Sessions
	s.runtime.Sessions, s.data.Sessions = maps.Clone(oldRuntime), maps.Clone(oldStored)
	restore := func() { s.runtime.Sessions, s.data.Sessions = oldRuntime, oldStored }
	for _, sess := range next {
		cp := cloneSession(sess)
		normalizeSessionValues(cp)
		cp.UpdatedAt = time.Now().Unix()
		s.runtime.Sessions[cp.Key] = cp
		s.syncPersistentSessionLocked(cp)
	}
	if err := s.saveLocked(); err != nil {
		restore()
		return err
	}
	if err := persistConfig(); err != nil {
		restore()
		if rollback := s.saveLocked(); rollback != nil {
			return errors.Join(err, rollback)
		}
		return err
	}
	return nil
}
