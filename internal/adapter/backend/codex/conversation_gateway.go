package codex

import (
	"context"
	"feidex/internal/application/backendops"
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
	StartParams  func(usecase.Request) backendops.ThreadStartConfig
	ResumeConfig func(*conversation.Session) map[string]any
	ForkParams   func(usecase.Request) backendops.ThreadForkRequest
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
	if g.StartParams == nil {
		return usecase.Thread{}, fmt.Errorf("codex thread start configuration not initialized")
	}
	result, err := StartThread(ctx, client, g.StartParams(r))
	if err != nil {
		return usecase.Thread{}, err
	}
	return usecase.Thread{ID: result.ID, Name: result.Name, Preview: result.Preview}, nil
}

func (g ConversationGateway) Resume(ctx context.Context, r usecase.Request) (usecase.Thread, error) {
	client, err := g.client()
	if err != nil {
		return usecase.Thread{}, err
	}
	resumeConfig := map[string]any(nil)
	if g.ResumeConfig != nil {
		resumeConfig = g.ResumeConfig(r.Session)
	}
	result, err := ResumeThread(ctx, client, r.Selection.ThreadID, r.Model, resumeConfig)
	if err != nil {
		return usecase.Thread{}, err
	}
	name, preview := textutil.FirstNonEmpty(r.Selection.Name, result.Name), textutil.FirstNonEmpty(r.Selection.Preview, result.Preview)
	if !r.SelectionExplicit {
		name = textutil.FirstNonEmpty(result.Name, r.Selection.Name)
		preview = textutil.FirstNonEmpty(result.Preview, r.Selection.Preview)
	}
	return usecase.Thread{
		ID:      textutil.FirstNonEmpty(strings.TrimSpace(result.ID), r.Selection.ThreadID),
		Name:    name,
		Preview: preview, Applied: &result.Applied,
	}, nil
}

func (g ConversationGateway) Fork(ctx context.Context, r usecase.Request) (usecase.Thread, error) {
	client, err := g.client()
	if err != nil {
		return usecase.Thread{}, err
	}
	if g.ForkParams == nil {
		return usecase.Thread{}, fmt.Errorf("codex thread fork configuration not initialized")
	}
	var result codexrpc.ThreadStartResult
	if err := client.Call(ctx, "thread/fork", ThreadForkParams(g.ForkParams(r)), &result); err != nil {
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
