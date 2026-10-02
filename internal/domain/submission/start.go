// Package submission owns submission state and transition invariants.
package submission

// StartState coalesces requests to start queued work while a start is already
// in progress. Runtime owns serialization; this value owns the transition.
type StartState struct {
	Running bool
	Pending bool
}

func (s StartState) Begin() (next StartState, admitted bool) {
	if s.Running {
		s.Pending = true
		return s, false
	}
	return StartState{Running: true}, true
}

func (s StartState) Finish() (next StartState, retry bool) {
	return StartState{}, s.Pending
}
