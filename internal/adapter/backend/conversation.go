// Package backend selects semantic backend gateways at execution time.
package backend

import (
	"context"
	"fmt"

	usecase "feidex/internal/application/conversation"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/workspace"
)

type ConversationGateway struct {
	Selected func() string
	Gateways map[string]usecase.Gateway
}

func (g ConversationGateway) gateway() (usecase.Gateway, error) {
	kind := g.Selected()
	gateway := g.Gateways[kind]
	if gateway == nil {
		return nil, fmt.Errorf("%s backend not initialized", kind)
	}
	return gateway, nil
}

func (g ConversationGateway) List(ctx context.Context, ws *workspace.Workspace, all bool) ([]conversation.ThreadEntry, error) {
	p, err := g.gateway()
	if err != nil {
		return nil, err
	}
	return p.List(ctx, ws, all)
}
func (g ConversationGateway) Start(ctx context.Context, r usecase.Request) (usecase.Thread, error) {
	p, err := g.gateway()
	if err != nil {
		return usecase.Thread{}, err
	}
	return p.Start(ctx, r)
}
func (g ConversationGateway) Resume(ctx context.Context, r usecase.Request) (usecase.Thread, error) {
	p, err := g.gateway()
	if err != nil {
		return usecase.Thread{}, err
	}
	return p.Resume(ctx, r)
}
func (g ConversationGateway) Fork(ctx context.Context, r usecase.Request) (usecase.Thread, error) {
	p, err := g.gateway()
	if err != nil {
		return usecase.Thread{}, err
	}
	return p.Fork(ctx, r)
}
func (g ConversationGateway) Interrupt(ctx context.Context, s *conversation.Session) error {
	p, err := g.gateway()
	if err != nil {
		return err
	}
	return p.Interrupt(ctx, s)
}
func (g ConversationGateway) Steer(ctx context.Context, s *conversation.Session, text string) error {
	p, err := g.gateway()
	if err != nil {
		return err
	}
	return p.Steer(ctx, s, text)
}
