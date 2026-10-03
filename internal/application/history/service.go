// Package history owns scoped transcript queries, turn selection and pagination.
package history

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"feidex/internal/application/backendops"
	"feidex/internal/application/presentation"
	"feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/textutil"
)

const PageSize = 50

type Repository interface {
	Session(string) *conversation.Session
}
type Reader interface {
	ReadConversationHistory(context.Context, string) (backendops.ThreadHistory, error)
}

type Service struct {
	Frontend   identity.FrontendID
	Repository Repository
	Backend    func() string
	Readers    map[string]Reader
}

type Snapshot struct {
	Backend, ID, Label string
	Turns              []backendops.HistoryTurn
}
type Page struct {
	Snapshot
	Number, Start, End int
}
type Detail struct {
	Snapshot
	Index int
}

func (s Service) Snapshot(ctx context.Context, key string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	frontend, _, _, _, _ := identity.ParseSessionKey(key)
	owner := strings.TrimSpace(string(s.Frontend))
	if frontend != owner {
		return Snapshot{}, fmt.Errorf("history session belongs to another frontend")
	}
	if s.Repository == nil {
		return Snapshot{}, fmt.Errorf("store not initialized")
	}
	if s.Backend == nil {
		return Snapshot{}, fmt.Errorf("backend not initialized")
	}
	kind := s.Backend()
	sess := s.Repository.Session(key)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		if kind == backend.BackendClaude {
			return Snapshot{}, fmt.Errorf("当前没有活动会话")
		}
		return Snapshot{}, fmt.Errorf("当前没有活动线程")
	}
	reader := s.Readers[kind]
	if reader == nil {
		return Snapshot{}, fmt.Errorf("history unavailable for backend %q", kind)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	thread, err := reader.ReadConversationHistory(ctx, strings.TrimSpace(sess.ActiveThreadID))
	if err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	// Query views own their memory. Readers may reuse immutable cached history;
	// marking a current turn must never mutate that cache or another query view.
	thread.Turns = slices.Clone(thread.Turns)
	for i := range thread.Turns {
		turn := &thread.Turns[i]
		turn.Inputs, turn.Outputs = slices.Clone(turn.Inputs), slices.Clone(turn.Outputs)
		turn.Records = slices.Clone(turn.Records)
		for j := range turn.Records {
			turn.Records[j].Details = slices.Clone(turn.Records[j].Details)
		}
	}
	label := presentation.CurrentThreadLabel(sess.ActiveThreadName, sess.ActiveThreadPreview, sess.ActiveThreadID)
	if label == "-" {
		label = textutil.FirstNonEmpty(thread.Name, thread.Preview, thread.ID)
	}
	for i := range thread.Turns {
		turn := &thread.Turns[i]
		if kind == backend.BackendClaude {
			turn.IsCurrent = i == 0 && conversation.HasInFlightSubmission(sess)
			if turn.IsCurrent {
				turn.Status = "running"
			}
		} else {
			turn.IsCurrent = strings.TrimSpace(turn.ID) != "" && strings.TrimSpace(turn.ID) == strings.TrimSpace(sess.ActiveTurnID)
		}
	}
	return Snapshot{Backend: kind, ID: thread.ID, Label: label, Turns: thread.Turns}, nil
}

func (s Service) Page(ctx context.Context, key string, number int) (Page, error) {
	view, err := s.Snapshot(ctx, key)
	if err != nil {
		return Page{}, err
	}
	if number < 0 {
		number = 0
	}
	total := len(view.Turns)
	if total == 0 {
		number = 0
	} else if number > (total-1)/PageSize {
		number = (total - 1) / PageSize
	}
	start := number * PageSize
	end := min(start+PageSize, total)
	return Page{Snapshot: view, Number: number, Start: start, End: end}, nil
}

func (s Service) Detail(ctx context.Context, key string, index int) (Detail, error) {
	view, err := s.Snapshot(ctx, key)
	if err != nil {
		return Detail{}, err
	}
	if index < 0 || index >= len(view.Turns) {
		return Detail{}, fmt.Errorf("history turn index out of range")
	}
	return Detail{Snapshot: view, Index: index}, nil
}

func (s Service) DetailForOrdinal(ctx context.Context, key string, ordinal int) (Detail, error) {
	view, err := s.Snapshot(ctx, key)
	if err != nil {
		return Detail{}, err
	}
	for i, turn := range view.Turns {
		if turn.Ordinal == ordinal {
			return Detail{Snapshot: view, Index: i}, nil
		}
	}
	return Detail{}, fmt.Errorf("Turn #%d 不存在", ordinal)
}
