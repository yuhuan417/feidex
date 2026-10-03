package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"feidex/internal/application"
	"feidex/internal/application/backendops"
	interactionapp "feidex/internal/application/interaction"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/interaction"
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

func TestEffectRunnerCardIdentityPreservesResultAndUnkeyedNavigation(t *testing.T) {
	calls := 0
	runner := EffectRunner{Deduper: NewMemoryEffectDeduper(), SendCardWithID: func(context.Context, application.SendCard) (string, error) { calls++; return "message", nil }}
	effect := application.SendCard{IdempotencyKey: "form-1"}
	first, err := runner.RunSendCard(context.Background(), effect)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(context.Background(), []application.Effect{effect}); err != nil {
		t.Fatal(err)
	}
	second, err := runner.RunSendCard(context.Background(), effect)
	if err != nil || first != "message" || second != first || calls != 1 {
		t.Fatalf("first=%q second=%q calls=%d error=%v", first, second, calls, err)
	}
	for range 2 {
		if _, err := runner.RunSendCard(context.Background(), application.SendCard{}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 3 {
		t.Fatalf("navigation sends=%d", calls)
	}
}

type blockingDeduperContext struct {
	context.Context
	registered chan struct{}
}

func (c blockingDeduperContext) Done() <-chan struct{} {
	c.registered <- struct{}{}
	return c.Context.Done()
}

func TestMemoryEffectDeduperBroadcastsFailureToEveryWaiter(t *testing.T) {
	d := NewMemoryEffectDeduper()
	started, release, registered := make(chan struct{}), make(chan struct{}), make(chan struct{}, 8)
	failure := errors.New("transport failure")
	var calls atomic.Int32
	fn := func() (any, error) { calls.Add(1); close(started); <-release; return "result", failure }
	results := make(chan error, 9)
	go func() { _, err := d.Do(context.Background(), "key", fn); results <- err }()
	<-started
	for range 8 {
		go func() {
			value, err := d.Do(blockingDeduperContext{Context: context.Background(), registered: registered}, "key", fn)
			if value != "result" {
				t.Errorf("waiter result=%v", value)
			}
			results <- err
		}()
	}
	for range 8 {
		<-registered
	}
	close(release)
	for range 9 {
		if err := <-results; !errors.Is(err, failure) {
			t.Errorf("waiter error=%v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("external calls=%d", calls.Load())
	}
}

type retryDeliveryRepository struct {
	request    *interaction.PendingRequest
	failUpdate bool
}

func (r *retryDeliveryRepository) Pending(string) *interaction.PendingRequest {
	if r.request == nil {
		return nil
	}
	cp := *r.request
	return &cp
}
func (r *retryDeliveryRepository) SavePending(req *interaction.PendingRequest) error {
	cp := *req
	r.request = &cp
	return nil
}
func (r *retryDeliveryRepository) UpdatePending(_ string, fn func(*interaction.PendingRequest)) error {
	if r.failUpdate {
		return errors.New("disk full")
	}
	fn(r.request)
	return nil
}
func (*retryDeliveryRepository) SetSubmissionStatus(string, string) error        { return nil }
func (*retryDeliveryRepository) SaveMessageLink(*conversation.MessageLink) error { return nil }

type runnerInteractionPresenter struct{ runner EffectRunner }

func (p runnerInteractionPresenter) DeliverInteraction(ctx context.Context, input interactionapp.DeliveryInput) (string, error) {
	return p.runner.RunSendCard(ctx, application.SendCard{IdempotencyKey: application.StableEffectKey("interaction-card", input.Request.FrontendID, input.Request.ID)})
}

func TestInteractionDeliverySaveFailureRetriesSameRealEffectResult(t *testing.T) {
	calls := 0
	runner := EffectRunner{Deduper: NewMemoryEffectDeduper(), SendCardWithID: func(context.Context, application.SendCard) (string, error) { calls++; return "card-1", nil }}
	repo := &retryDeliveryRepository{failUpdate: true}
	service := interactionapp.DeliveryService{Repository: repo}
	input := interactionapp.DeliveryInput{Request: interaction.PendingRequest{ID: "form-1", FrontendID: "frontend"}, NonBlocking: true}
	if err := service.Open(context.Background(), input, runnerInteractionPresenter{runner}); err == nil {
		t.Fatal("expected message association save failure")
	}
	repo.failUpdate = false
	if err := service.Open(context.Background(), input, runnerInteractionPresenter{runner}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || repo.request.FeishuMsgID != "card-1" {
		t.Fatalf("calls=%d request=%+v", calls, repo.request)
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
