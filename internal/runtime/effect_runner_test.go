package runtime

import (
	"context"
	"errors"
	"testing"

	"feidex/internal/application"
)

func TestEffectRunnerSaveFailurePreventsBackendStart(t *testing.T) {
	failure := errors.New("disk full")
	started := false
	runner := EffectRunner{
		Save:  func(context.Context, application.SaveState) error { return failure },
		Start: func(context.Context, application.StartTurn) error { started = true; return nil },
	}
	err := runner.Run(context.TODO(), []application.Effect{application.SaveState{}, application.StartTurn{}})
	if !errors.Is(err, failure) || started {
		t.Fatalf("error=%v started=%v", err, started)
	}
}

func TestEffectRunnerCancelledFrontendDoesNotSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	runner := EffectRunner{Send: func(context.Context, application.SendMessage) error { called = true; return nil }}
	if err := runner.Run(ctx, []application.Effect{application.SendMessage{}}); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("error=%v called=%v", err, called)
	}
}
