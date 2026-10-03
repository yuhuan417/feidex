package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"

	"feidex/internal/application"
	"feidex/internal/application/backendops"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/submission"
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

func TestEffectRunnerDeduplicatesSuccessfulTurnStartBySubmission(t *testing.T) {
	count := 0
	runner := EffectRunner{
		Deduper: NewMemoryEffectDeduper(),
		StartWithResult: func(context.Context, application.StartTurn) (backendops.TurnResult, error) {
			count++
			return backendops.TurnResult{ID: "turn-1"}, nil
		},
	}
	effect := application.StartTurn{Frontend: identity.FrontendID("front"), SessionKey: identity.SessionKey("session"), Request: backendops.StartTurnRequest{Submission: &submission.Submission{ID: "submission-1"}}}
	first, err := runner.RunStartTurn(context.Background(), effect)
	if err != nil || first.ID != "turn-1" {
		t.Fatalf("first result = %+v, %v", first, err)
	}
	second, err := runner.RunStartTurn(context.Background(), effect)
	if err != nil || second.ID != "turn-1" || count != 1 {
		t.Fatalf("second result = %+v, %v, calls=%d", second, err, count)
	}
}

func TestMemoryEffectDeduperAllowsRetryAfterFailure(t *testing.T) {
	deduper := NewMemoryEffectDeduper()
	count := 0
	fn := func() (any, error) {
		count++
		if count == 1 {
			return nil, errors.New("temporary")
		}
		return "ok", nil
	}
	if _, err := deduper.Do(context.Background(), "key", fn); err == nil {
		t.Fatal("first attempt should fail")
	}
	value, err := deduper.Do(context.Background(), "key", fn)
	if err != nil || value != "ok" || count != 2 {
		t.Fatalf("retry = %#v, %v, calls=%d", value, err, count)
	}
}

func TestMemoryEffectDeduperCoordinatesConcurrentRetry(t *testing.T) {
	deduper := NewMemoryEffectDeduper()
	count := 0
	var mu sync.Mutex
	fn := func() (any, error) {
		mu.Lock()
		count++
		mu.Unlock()
		return "ok", nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := deduper.Do(context.Background(), "same-key", fn)
			if err != nil || value != "ok" {
				t.Errorf("dedupe result = %#v, %v", value, err)
			}
		}()
	}
	wg.Wait()
	if count != 1 {
		t.Fatalf("calls=%d, want one", count)
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
