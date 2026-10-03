package claude

import (
	"context"
	"feidex/internal/adapter/backend/claude/catalog"
	usecase "feidex/internal/application/conversation"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/workspace"
	"feidex/internal/textutil"
	"fmt"
	"strings"
)

type ConversationClient interface {
	EnsureSession(context.Context, string, *workspace.Workspace, string, string) (string, error)
	ForkSession(context.Context, string, *workspace.Workspace, string, string) (string, error)
	ResetSession(string) error
	Interrupt(context.Context, string) error
}

type ConversationGateway struct {
	Client   func() ConversationClient
	Continue func(string, string) error
}

func (g ConversationGateway) List(_ context.Context, ws *workspace.Workspace, all bool) ([]conversation.ThreadEntry, error) {
	return catalog.ListSessions("", ws, all)
}
func (g ConversationGateway) Start(ctx context.Context, r usecase.Request) (usecase.Thread, error) {
	client := g.Client()
	if client == nil {
		return usecase.Thread{}, fmt.Errorf("claude backend not initialized")
	}
	_ = client.ResetSession(r.SessionKey)
	id, err := client.EnsureSession(ctx, r.SessionKey, r.Workspace, "", r.Model)
	return usecase.Thread{ID: id, Name: "Claude", Preview: textutil.FirstNonEmpty(r.Session.ActiveThreadPreview, r.Workspace.Name)}, err
}
func (g ConversationGateway) Resume(ctx context.Context, r usecase.Request) (usecase.Thread, error) {
	client := g.Client()
	if client == nil {
		return usecase.Thread{}, fmt.Errorf("claude backend not initialized")
	}
	sel := r.Selection
	var entry *conversation.ThreadEntry
	var err error
	if r.SelectionExplicit {
		entry, err = catalog.FindSessionEntry(sel.ThreadID)
	}
	if err != nil {
		return usecase.Thread{}, err
	}
	if entry != nil {
		sel.Name = textutil.FirstNonEmpty(sel.Name, entry.Name)
		sel.Preview = textutil.FirstNonEmpty(sel.Preview, entry.Preview)
		sel.Cwd = textutil.FirstNonEmpty(sel.Cwd, entry.Cwd)
	}
	if strings.TrimSpace(sel.Cwd) != "" && !conversation.SameWorkspaceCWD(sel.Cwd, r.Workspace.Cwd) {
		return usecase.Thread{}, conversation.NewWarning("该会话不属于当前工作区，请先切换 workspace")
	}
	id, err := client.EnsureSession(ctx, r.SessionKey, r.Workspace, sel.ThreadID, r.Model)
	return usecase.Thread{ID: id, Name: textutil.FirstNonEmpty(sel.Name, "Claude"), Preview: textutil.FirstNonEmpty(sel.Preview, r.Workspace.Name)}, err
}
func (g ConversationGateway) Fork(ctx context.Context, r usecase.Request) (usecase.Thread, error) {
	client := g.Client()
	if client == nil {
		return usecase.Thread{}, fmt.Errorf("claude backend not initialized")
	}
	id, err := client.ForkSession(ctx, r.SessionKey, r.Workspace, r.Session.ActiveThreadID, r.Model)
	return usecase.Thread{ID: id, Name: textutil.FirstNonEmpty(r.Session.ActiveThreadName, "Claude"), Preview: textutil.FirstNonEmpty(r.Session.ActiveThreadPreview, r.Workspace.Name)}, err
}
func (g ConversationGateway) Interrupt(ctx context.Context, sess *conversation.Session) error {
	client := g.Client()
	if client == nil {
		return fmt.Errorf("claude backend not initialized")
	}
	return client.Interrupt(ctx, sess.Key)
}
func (g ConversationGateway) Steer(_ context.Context, sess *conversation.Session, text string) error {
	if g.Continue == nil {
		return fmt.Errorf("claude continuation unavailable")
	}
	return g.Continue(sess.Key, text)
}
