package autoretry

import (
	"errors"
	"testing"
	"time"
)

type delayedTask struct{ stopped bool }

func (t *delayedTask) Stop() bool { t.stopped = true; return true }

func TestDisablePersistsBeforeCancelingRetries(t *testing.T) {
	for _, failSave := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved", true: "save_failed"}[failSave], func(t *testing.T) {
			task := &delayedTask{}
			tracker := NewTracker(func(time.Duration, func()) DelayedTask { return task })
			tracker.States["session"] = &RetryState{SessionKey: "session", Timer: task}
			cause := errors.New("cannot save settings")
			engine := NewEngine(Dependencies{Tracker: tracker, SaveEnabled: func(enabled bool) error {
				if enabled {
					t.Fatal("expected disabled setting")
				}
				if task.stopped || tracker.States["session"] == nil {
					t.Fatal("retry canceled before persistence")
				}
				if failSave {
					return cause
				}
				return nil
			}})
			err := engine.UpdateAutoRetryEnabled(false)
			if failSave {
				if !errors.Is(err, cause) || task.stopped || !engine.HasPendingAutoRetry("session") {
					t.Fatalf("failed save changed retry: err=%v stopped=%v", err, task.stopped)
				}
			} else if err != nil || !task.stopped || engine.HasBlockingAutoRetry("") {
				t.Fatalf("saved disable did not clear retry: err=%v stopped=%v", err, task.stopped)
			}
		})
	}
}

func TestEnablePreservesExistingRetry(t *testing.T) {
	task := &delayedTask{}
	tracker := NewTracker(func(time.Duration, func()) DelayedTask { return task })
	tracker.States["session"] = &RetryState{SessionKey: "session", Timer: task}
	engine := NewEngine(Dependencies{Tracker: tracker, SaveEnabled: func(enabled bool) error {
		if !enabled {
			t.Fatal("expected enabled setting")
		}
		return nil
	}})
	if err := engine.UpdateAutoRetryEnabled(true); err != nil {
		t.Fatal(err)
	}
	if task.stopped || !engine.HasPendingAutoRetry("session") {
		t.Fatal("enabling reset existing retry")
	}
}

func TestRetrySchedulingUsesInjectedRuntime(t *testing.T) {
	task := &delayedTask{}
	called := false
	tracker := NewTracker(func(delay time.Duration, fn func()) DelayedTask {
		if delay != 2*time.Second {
			t.Fatalf("delay = %v", delay)
		}
		fn()
		return task
	})
	engine := NewEngine(Dependencies{Tracker: tracker})
	if got := engine.ScheduleDelayedTask(2*time.Second, func() { called = true }); got != task || !called {
		t.Fatal("runtime scheduler was not used")
	}
}
