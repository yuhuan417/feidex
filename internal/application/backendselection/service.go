package backendselection

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
)

type AvailableBackend struct{ Kind, Command, Path string }
type RuntimeHandle struct {
	Close   func() error
	Install func()
}

type Repository interface {
	ConfiguredBackend() string
	Sessions() []*conversation.Session
	CommitBackend(string, []*conversation.Session, []*conversation.Session) error
}

type Transition interface {
	LockSwitch()
	UnlockSwitch()
	BeginBackendSwitchState(string)
	FinishBackendSwitchState()
}

type Runtime interface {
	AvailableBackends() []AvailableBackend
	Ready(string) bool
	IdleBlockedReason() string
	Prepare(context.Context, string) (*RuntimeHandle, error)
	Snapshot() *RuntimeHandle
	Recover()
}

type Dependencies struct {
	Repository Repository
	Transition Transition
	Runtime    Runtime
}

type Service Dependencies

func NewService(deps Dependencies) Service { return Service(deps) }

func (s Service) Available(target string) bool {
	target = backend.NormalizeBackend(target)
	if target == "" {
		return false
	}
	for _, candidate := range s.Runtime.AvailableBackends() {
		if candidate.Kind == target {
			return true
		}
	}
	return false
}

func (s Service) Switch(ctx context.Context, target string) error {
	target = backend.NormalizeBackend(target)
	if target == "" {
		return fmt.Errorf("missing backend")
	}
	if !s.Available(target) {
		return fmt.Errorf("%s backend 当前不可用", target)
	}
	s.Transition.LockSwitch()
	defer s.Transition.UnlockSwitch()
	if reason := s.Runtime.IdleBlockedReason(); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	current := s.Repository.ConfiguredBackend()
	if current == target && s.Runtime.Ready(target) {
		return nil
	}
	s.Transition.BeginBackendSwitchState(target)
	defer s.Transition.FinishBackendSwitchState()
	expected := s.Repository.Sessions()
	next := SessionsAfterSwitch(expected, current, target)
	prepared, err := s.Runtime.Prepare(ctx, target)
	if err != nil {
		return err
	}
	if prepared == nil || prepared.Install == nil || prepared.Close == nil {
		return fmt.Errorf("prepared backend runtime is incomplete")
	}
	old := s.Runtime.Snapshot()
	if err := s.Repository.CommitBackend(target, expected, next); err != nil {
		_ = prepared.Close()
		return err
	}
	prepared.Install()
	s.Runtime.Recover()
	if old != nil && old.Close != nil {
		_ = old.Close()
	}
	return nil
}

func SessionsAfterSwitch(sessions []*conversation.Session, current, target string) []*conversation.Session {
	out := make([]*conversation.Session, 0, len(sessions))
	for _, sess := range sessions {
		cp := conversation.CloneSession(sess)
		if cp == nil {
			continue
		}
		if current != "" {
			conversation.StoreBackendThread(cp, current)
		}
		if !conversation.RestoreBackendThread(cp, target) {
			conversation.ClearThreadContext(cp)
		}
		if strings.TrimSpace(cp.Status) == "" {
			cp.Status = "idle"
		}
		out = append(out, cp)
	}
	return out
}
