// Package autoretry provides pure types and functions for the auto-retry subsystem.
package autoretry

import (
	"fmt"
	"strings"
	"sync"
	"time"

	apputil "feidex/internal/app/apputil"
	"feidex/internal/state"
)

const (
	InitialDelay = 1 * time.Second
	MaxDelay     = 15 * time.Second
)

// DelayedTask is an interface for a cancellable delayed task.
type DelayedTask interface {
	Stop() bool
}

// Tracker tracks auto-retry state by session key.
type Tracker struct {
	Mu     sync.Mutex
	States map[string]*RetryState
	After  func(time.Duration, func()) DelayedTask
}

// NewTracker creates a new auto-retry tracker.
func NewTracker() *Tracker {
	return &Tracker{States: map[string]*RetryState{}}
}

// RetryState holds the state for a single auto-retry operation.
type RetryState struct {
	SessionKey           string
	ThreadID             string
	WorkspaceID          string
	ChatID               string
	TriggerMessageID     string
	SourceRootMessageIDs []string
	RetryCount           int
	BackoffStep          int
	StatusMessageID      string
	Timer                DelayedTask
	TimerSeq             uint64
	Canceled             bool
}

// DelayForStep calculates the backoff delay for a given retry step.
func DelayForStep(step int) time.Duration {
	delay := InitialDelay
	for i := 0; i < step; i++ {
		if delay >= MaxDelay {
			return MaxDelay
		}
		delay *= 2
	}
	if delay > MaxDelay {
		return MaxDelay
	}
	return delay
}

// FormatDelay formats a duration as a human-readable delay string.
func FormatDelay(delay time.Duration) string {
	if delay <= 0 {
		return "0s"
	}
	if delay < time.Second {
		return "<1s"
	}
	return fmt.Sprintf("%ds", int64((delay+500*time.Millisecond)/time.Second))
}

// CloneState deep-copies a RetryState, clearing the Timer.
func CloneState(src *RetryState) RetryState {
	if src == nil {
		return RetryState{}
	}
	cp := *src
	cp.SourceRootMessageIDs = append([]string(nil), src.SourceRootMessageIDs...)
	cp.Timer = nil
	return cp
}

// StateWaiting returns true if the state has a pending timer.
func StateWaiting(state *RetryState) bool {
	return state != nil && !state.Canceled && state.Timer != nil
}

// RefreshState updates a RetryState from session and submission data.
func RefreshState(state *RetryState, sess *state.Session, sub *state.Submission, threadID string) {
	if state == nil {
		return
	}
	if strings.TrimSpace(threadID) != "" {
		state.ThreadID = strings.TrimSpace(threadID)
	}
	if sub != nil {
		state.WorkspaceID = apputil.FirstNonEmpty(strings.TrimSpace(sub.WorkspaceID), state.WorkspaceID)
		state.ChatID = apputil.FirstNonEmpty(strings.TrimSpace(sub.ChatID), state.ChatID)
		state.TriggerMessageID = apputil.FirstNonEmpty(strings.TrimSpace(sub.TriggerMessageID), state.TriggerMessageID)
		if len(sub.SourceRootMessageIDs) > 0 {
			state.SourceRootMessageIDs = append([]string(nil), sub.SourceRootMessageIDs...)
		}
	}
	if sess != nil {
		state.WorkspaceID = apputil.FirstNonEmpty(strings.TrimSpace(sess.WorkspaceID), state.WorkspaceID)
		state.ChatID = apputil.FirstNonEmpty(strings.TrimSpace(sess.ChatID), state.ChatID)
		if rootMessageID := strings.TrimSpace(sess.RootMessageID); len(state.SourceRootMessageIDs) == 0 && rootMessageID != "" {
			state.SourceRootMessageIDs = []string{rootMessageID}
		}
		if strings.TrimSpace(state.TriggerMessageID) == "" {
			state.TriggerMessageID = strings.TrimSpace(sess.RootMessageID)
		}
	}
}
