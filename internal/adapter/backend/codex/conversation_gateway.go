package codex

import (
	"context"
	usecase "feidex/internal/application/conversation"
	"feidex/internal/codexrpc"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/workspace"
	"feidex/internal/textutil"
	"fmt"
	"strings"
)

// ConversationGateway maps semantic operations to Codex RPC. Configuration
// providers supply resolved values; the adapter never locates another service.
type ConversationGateway struct {
	Client       func() (ConversationClient, error)
	StartParams  func(usecase.Request) codexrpc.ThreadStartParams
	ResumeConfig func(*conversation.Session) map[string]any
	ForkParams   func(usecase.Request) map[string]any
}

func (g ConversationGateway) client() (ConversationClient, error) {
	if g.Client == nil {
		return nil, fmt.Errorf("codex client not initialized")
	}
	return g.Client()
}

func (g ConversationGateway) List(ctx context.Context, ws *workspace.Workspace, all bool) ([]conversation.ThreadEntry, error) {
	client, err := g.client()
	if err != nil {
		return nil, err
	}
	sources := []string{"appServer"}
	if all {
		sources = []string{"appServer", "cli", "vscode", "exec"}
	}
	queries := []map[string]any{
		{"limit": 8, "cwd": ws.Cwd, "archived": false, "sourceKinds": sources},
		{"limit": 8, "cwd": ws.Cwd, "archived": false},
		{"limit": 8, "archived": false},
	}
	var result codexrpc.ThreadListResult
	for _, query := range queries {
		result = codexrpc.ThreadListResult{}
		err = client.Call(ctx, "thread/list", query, &result)
		if err == nil && len(result.Data) > 0 {
			break
		}
	}
	if err != nil && len(result.Data) == 0 {
		return nil, err
	}
	items := make([]conversation.ThreadEntry, 0, len(result.Data))
	for _, e := range result.Data {
		if conversation.SameWorkspaceCWD(e.Cwd, ws.Cwd) {
			items = append(items, conversation.ThreadEntry(e))
		}
	}
	return items, nil
}

func (g ConversationGateway) Start(ctx context.Context, r usecase.Request) (usecase.Thread, error) {
	client, err := g.client()
	if err != nil {
		return usecase.Thread{}, err
	}
	var result codexrpc.ThreadStartResult
	if err := client.Call(ctx, "thread/start", g.StartParams(r).Map(), &result); err != nil {
		return usecase.Thread{}, err
	}
	return usecase.Thread{ID: result.Thread.ID, Name: result.Thread.Name, Preview: result.Thread.Preview}, nil
}

func (g ConversationGateway) Resume(ctx context.Context, r usecase.Request) (usecase.Thread, error) {
	client, err := g.client()
	if err != nil {
		return usecase.Thread{}, err
	}
	params := codexrpc.ThreadResumeParams{ThreadID: r.Selection.ThreadID, PersistExtendedHistory: true, Model: r.Model}
	if g.ResumeConfig != nil {
		params.Config = g.ResumeConfig(r.Session)
	}
	var result codexrpc.ThreadStartResult
	if err := client.Call(ctx, "thread/resume", params.Map(), &result); err != nil {
		return usecase.Thread{}, err
	}
	name, preview := textutil.FirstNonEmpty(r.Selection.Name, result.Thread.Name), textutil.FirstNonEmpty(r.Selection.Preview, result.Thread.Preview)
	if !r.SelectionExplicit {
		name = textutil.FirstNonEmpty(result.Thread.Name, r.Selection.Name)
		preview = textutil.FirstNonEmpty(result.Thread.Preview, r.Selection.Preview)
	}
	applied := ResumedThreadConfig(params.Model, params.Config)
	return usecase.Thread{
		ID:      textutil.FirstNonEmpty(strings.TrimSpace(result.Thread.ID), r.Selection.ThreadID),
		Name:    name,
		Preview: preview, Applied: &applied,
	}, nil
}

func (g ConversationGateway) Fork(ctx context.Context, r usecase.Request) (usecase.Thread, error) {
	client, err := g.client()
	if err != nil {
		return usecase.Thread{}, err
	}
	var result codexrpc.ThreadStartResult
	if err := client.Call(ctx, "thread/fork", g.ForkParams(r), &result); err != nil {
		return usecase.Thread{}, err
	}
	return usecase.Thread{ID: strings.TrimSpace(result.Thread.ID), Name: result.Thread.Name, Preview: result.Thread.Preview}, nil
}

func (g ConversationGateway) Interrupt(ctx context.Context, sess *conversation.Session) error {
	client, err := g.client()
	if err != nil {
		return err
	}
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveTurnID) == "" {
		return fmt.Errorf("当前没有运行中的任务")
	}
	return client.Call(ctx, "turn/interrupt", map[string]any{"threadId": sess.ActiveThreadID, "turnId": sess.ActiveTurnID}, nil)
}

func (g ConversationGateway) Steer(ctx context.Context, sess *conversation.Session, text string) error {
	client, err := g.client()
	if err != nil {
		return err
	}
	return client.Call(ctx, "turn/steer", map[string]any{
		"threadId": sess.ActiveThreadID, "expectedTurnId": sess.ActiveTurnID,
		"input": []map[string]any{{"type": "text", "text": text, "text_elements": []any{}}},
	}, nil)
}
