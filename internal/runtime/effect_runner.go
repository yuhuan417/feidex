package runtime

import (
	"context"
	"feidex/internal/application"
	"feidex/internal/application/backendops"
	"fmt"
)

// EffectRunner executes only explicit external ports after the use case has
// completed its state transition. It stops on the first failed effect, so an
// unsuccessful save cannot accidentally start a backend turn.
type EffectRunner struct {
	Deduper         EffectDeduper
	RefreshGroup    func(context.Context, application.RefreshGroupStatus) error
	Steer           func(context.Context, application.SteerTurn) error
	Enqueue         func(context.Context, application.EnqueueInput) error
	Save            func(context.Context, application.SaveState) error
	Send            func(context.Context, application.SendMessage) error
	SendWithID      func(context.Context, application.SendMessage) (string, error)
	SendCard        func(context.Context, application.SendCard) error
	SendCardWithID  func(context.Context, application.SendCard) (string, error)
	Patch           func(context.Context, application.PatchCard) error
	Start           func(context.Context, application.StartTurn) error
	StartWithResult func(context.Context, application.StartTurn) (backendops.TurnResult, error)
	Resolve         func(context.Context, application.ResolveBackendRequest) error
}

// WithoutDeduper returns a transport-only runner for imperative adapter
// calls. Application effects use the frontend-scoped deduper; direct client
// methods such as command capture and permission retries must retain their
// historical one-call semantics.
func (r EffectRunner) WithoutDeduper() EffectRunner {
	r.Deduper = nil
	return r
}

func (r EffectRunner) run(ctx context.Context, key string, fn func() error) error {
	if r.Deduper == nil || key == "" {
		return fn()
	}
	_, err := r.Deduper.Do(ctx, key, func() (any, error) { return nil, fn() })
	return err
}

func (r EffectRunner) runValue(ctx context.Context, key string, fn func() (any, error)) (any, error) {
	if r.Deduper == nil || key == "" {
		return fn()
	}
	return r.Deduper.Do(ctx, key, fn)
}

func (r EffectRunner) RunSendMessage(ctx context.Context, effect application.SendMessage) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, err := r.runValue(ctx, application.EffectIdentity(effect), func() (any, error) {
		if r.SendWithID != nil {
			return r.SendWithID(ctx, effect)
		}
		if r.Send == nil {
			return "", fmt.Errorf("send effect unavailable")
		}
		return "", r.Send(ctx, effect)
	})
	if err != nil {
		return "", err
	}
	result, _ := value.(string)
	return result, nil
}

func (r EffectRunner) RunSendCard(ctx context.Context, effect application.SendCard) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, err := r.runValue(ctx, application.EffectIdentity(effect), func() (any, error) {
		if r.SendCardWithID != nil {
			return r.SendCardWithID(ctx, effect)
		}
		if r.SendCard == nil {
			return "", fmt.Errorf("send card effect unavailable")
		}
		return "", r.SendCard(ctx, effect)
	})
	if err != nil {
		return "", err
	}
	result, _ := value.(string)
	return result, nil
}

func (r EffectRunner) Run(ctx context.Context, effects []application.Effect) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, effect := range effects {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		switch e := effect.(type) {
		case application.RefreshGroupStatus:
			if r.RefreshGroup == nil {
				return fmt.Errorf("group refresh effect unavailable")
			}
			err = r.run(ctx, application.EffectIdentity(e), func() error { return r.RefreshGroup(ctx, e) })
		case application.SaveState:
			if r.Save == nil {
				return fmt.Errorf("save effect unavailable")
			}
			err = r.run(ctx, application.EffectIdentity(e), func() error { return r.Save(ctx, e) })
		case application.SendMessage:
			if r.Send == nil {
				return fmt.Errorf("send effect unavailable")
			}
			err = r.run(ctx, application.EffectIdentity(e), func() error { return r.Send(ctx, e) })
		case application.SendCard:
			_, err = r.RunSendCard(ctx, e)
		case application.PatchCard:
			if r.Patch == nil {
				return fmt.Errorf("patch effect unavailable")
			}
			err = r.run(ctx, application.EffectIdentity(e), func() error { return r.Patch(ctx, e) })
		case application.StartTurn:
			_, err = r.RunStartTurn(ctx, e)
		case application.SteerTurn:
			if r.Steer == nil {
				return fmt.Errorf("steer effect unavailable")
			}
			err = r.run(ctx, application.EffectIdentity(e), func() error { return r.Steer(ctx, e) })
		case application.EnqueueInput:
			if r.Enqueue == nil {
				return fmt.Errorf("enqueue effect unavailable")
			}
			err = r.run(ctx, application.EffectIdentity(e), func() error { return r.Enqueue(ctx, e) })
		case application.ResolveBackendRequest:
			if r.Resolve == nil {
				return fmt.Errorf("resolve effect unavailable")
			}
			err = r.run(ctx, application.EffectIdentity(e), func() error { return r.Resolve(ctx, e) })
		default:
			return fmt.Errorf("unsupported effect %T", effect)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// RunStartTurn preserves the returned ID; notifications can still race the RPC.
func (r EffectRunner) RunStartTurn(ctx context.Context, effect application.StartTurn) (backendops.TurnResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return backendops.TurnResult{}, err
	}
	value, err := r.runValue(ctx, application.EffectIdentity(effect), func() (any, error) {
		if r.StartWithResult != nil {
			return r.StartWithResult(ctx, effect)
		}
		if r.Start == nil {
			return backendops.TurnResult{}, fmt.Errorf("start effect unavailable")
		}
		return backendops.TurnResult{}, r.Start(ctx, effect)
	})
	if err != nil {
		return backendops.TurnResult{}, err
	}
	result, _ := value.(backendops.TurnResult)
	return result, nil
}
