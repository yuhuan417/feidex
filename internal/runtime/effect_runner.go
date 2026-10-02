package runtime

import (
	"context"
	"feidex/internal/application"
	"fmt"
)

// EffectRunner executes only explicit external ports after the use case has
// completed its state transition. It stops on the first failed effect, so an
// unsuccessful save cannot accidentally start a backend turn.
type EffectRunner struct {
	Save           func(context.Context, application.SaveState) error
	Send           func(context.Context, application.SendMessage) error
	SendWithID     func(context.Context, application.SendMessage) (string, error)
	SendCard       func(context.Context, application.SendCard) error
	SendCardWithID func(context.Context, application.SendCard) (string, error)
	Patch          func(context.Context, application.PatchCard) error
	Start          func(context.Context, application.StartTurn) error
	Resolve        func(context.Context, application.ResolveBackendRequest) error
}

func (r EffectRunner) RunSendMessage(ctx context.Context, effect application.SendMessage) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r.SendWithID != nil {
		return r.SendWithID(ctx, effect)
	}
	if r.Send == nil {
		return "", fmt.Errorf("send effect unavailable")
	}
	return "", r.Send(ctx, effect)
}

func (r EffectRunner) RunSendCard(ctx context.Context, effect application.SendCard) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r.SendCardWithID != nil {
		return r.SendCardWithID(ctx, effect)
	}
	if r.SendCard == nil {
		return "", fmt.Errorf("send card effect unavailable")
	}
	return "", r.SendCard(ctx, effect)
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
		case application.SaveState:
			if r.Save == nil {
				return fmt.Errorf("save effect unavailable")
			}
			err = r.Save(ctx, e)
		case application.SendMessage:
			if r.Send == nil {
				return fmt.Errorf("send effect unavailable")
			}
			err = r.Send(ctx, e)
		case application.SendCard:
			if r.SendCard == nil {
				return fmt.Errorf("send card effect unavailable")
			}
			err = r.SendCard(ctx, e)
		case application.PatchCard:
			if r.Patch == nil {
				return fmt.Errorf("patch effect unavailable")
			}
			err = r.Patch(ctx, e)
		case application.StartTurn:
			if r.Start == nil {
				return fmt.Errorf("start effect unavailable")
			}
			err = r.Start(ctx, e)
		case application.ResolveBackendRequest:
			if r.Resolve == nil {
				return fmt.Errorf("resolve effect unavailable")
			}
			err = r.Resolve(ctx, e)
		default:
			return fmt.Errorf("unsupported effect %T", effect)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
