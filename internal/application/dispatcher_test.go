package application

import (
	"context"
	"testing"
)

func TestDispatcherRejectsCrossFrontendBeforeCallingOwner(t *testing.T) {
	called := false
	d := Dispatcher{Frontend: "a", Retry: func(context.Context, RetryTimerFired) (Result, error) {
		called = true
		return Result{}, nil
	}}
	if _, err := d.Dispatch(context.TODO(), RetryTimerFired{Frontend: "b", SessionKey: "session", Sequence: 3}); err == nil || called {
		t.Fatalf("foreign timer: error=%v called=%v", err, called)
	}
	if _, err := d.Dispatch(context.TODO(), RetryTimerFired{Frontend: " a ", SessionKey: "session", Sequence: 3}); err != nil || !called {
		t.Fatalf("own timer: error=%v called=%v", err, called)
	}
}
