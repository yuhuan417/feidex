// Package history adapts Feishu history commands and renders application query views.
package history

import (
	"context"
	historyapp "feidex/internal/application/history"
	"feidex/internal/feishu"
	"fmt"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// HistoryPageSize is the number of turns displayed per page.
	HistoryPageSize = historyapp.PageSize
	// HistoryCommandUsage is the usage string for the /history command.
	HistoryCommandUsage = "/history | /history detail TURN_NUMBER"
)

type Queries interface {
	Page(context.Context, string, int) (historyapp.Page, error)
	Detail(context.Context, string, int) (historyapp.Detail, error)
	DetailForOrdinal(context.Context, string, int) (historyapp.Detail, error)
}

type Outbound interface {
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
}
type Dependencies struct {
	Context       func() context.Context
	Outbound      Outbound
	Queries       Queries
	SessionKey    func(*feishu.InboundMessage) string
	ReplyInThread func(string) bool
}

// ---------------------------------------------------------------------------
// Service — manages /history command actions
// ---------------------------------------------------------------------------

// Service manages history command actions for a single app instance.
type Service struct{ deps Dependencies }

// NewService creates a new history service bound to the given app.
func NewService(deps Dependencies) Service { return Service{deps: deps} }

// ---------------------------------------------------------------------------
// Command handling
// ---------------------------------------------------------------------------

// CommandHistory handles the /history command with optional sub-commands.
func (s Service) CommandHistory(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		if len(args) != 2 || strings.TrimSpace(args[0]) != "detail" {
			return fmt.Errorf("usage: %s", HistoryCommandUsage)
		}
		ordinal, err := strconv.Atoi(strings.TrimSpace(args[1]))
		if err != nil || ordinal <= 0 {
			return fmt.Errorf("usage: %s", HistoryCommandUsage)
		}
		sessionKey := s.deps.SessionKey(msg)
		view, err := s.deps.Queries.DetailForOrdinal(s.context(), sessionKey, ordinal)
		if err != nil {
			return err
		}
		card := RenderDetail(sessionKey, view)
		_, err = s.deps.Outbound.ReplyCard(s.context(), msg.MessageID, card, s.deps.ReplyInThread(msg.ChatType))
		return err
	}
	sessionKey := s.deps.SessionKey(msg)
	card, err := s.RenderHistoryCard(sessionKey, 0)
	if err != nil {
		return err
	}
	_, err = s.deps.Outbound.ReplyCard(s.context(), msg.MessageID, card, s.deps.ReplyInThread(msg.ChatType))
	return err
}

// ---------------------------------------------------------------------------
// Delegation to conversation backend
// ---------------------------------------------------------------------------

// RenderHistoryCard renders the history list card for the given session and page.
func (s Service) RenderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	view, err := s.deps.Queries.Page(s.context(), sessionKey, page)
	if err != nil {
		return nil, err
	}
	return RenderPage(sessionKey, view), nil
}

// RenderHistoryDetailCard renders the history detail card for the given session
// and turn index.
func (s Service) RenderHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	view, err := s.deps.Queries.Detail(s.context(), sessionKey, index)
	if err != nil {
		return nil, err
	}
	return RenderDetail(sessionKey, view), nil
}

func (s Service) context() context.Context {
	if s.deps.Context != nil {
		return s.deps.Context()
	}
	return context.Background()
}
