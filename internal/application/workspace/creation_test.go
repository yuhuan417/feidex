package workspace

import (
	"context"
	"errors"
	"testing"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/routing"
	domain "feidex/internal/domain/workspace"
)

type creationFilesystem struct{}

func (creationFilesystem) ResolvePath(path string) string { return path }
func (creationFilesystem) MakeDirectory(string) error     { return nil }

type creationGit struct {
	cloneErr, worktreeErr error
	cloned, worktrees     int
}

func (g *creationGit) Clone(context.Context, string, string, func(string)) error {
	g.cloned++
	return g.cloneErr
}
func (g *creationGit) Worktree(context.Context, string, string, string) error {
	g.worktrees++
	return g.worktreeErr
}

func TestCreationCancellationDoesNotCreateDefinition(t *testing.T) {
	repo := &configurationRepositoryStub{workspaces: map[string]domain.Workspace{}}
	git := &creationGit{cloneErr: context.Canceled}
	svc := CreationService{Filesystem: creationFilesystem{}, Git: git, Lifecycle: &Lifecycle{Configuration: ConfigurationService{Repository: repo}}}
	_, err := svc.Clone(context.Background(), SwitchRequest{}, "repo", &ClonePlan{WorkspaceID: "new", TargetDir: "/new"}, nil)
	if Outcome(err) != CreationCancelled || len(repo.workspaces) != 0 {
		t.Fatalf("err=%v definitions=%v", err, repo.workspaces)
	}
}
func TestCreationRetainsDefinitionOnBindingFailure(t *testing.T) {
	repo := &configurationRepositoryStub{workspaces: map[string]domain.Workspace{}}
	state := &lifecycleRepositoryStub{failure: errors.New("commit failed")}
	svc := CreationService{Filesystem: creationFilesystem{}, Git: &creationGit{}, Lifecycle: &Lifecycle{Configuration: ConfigurationService{Repository: repo}, Repository: state}}
	result, err := svc.Worktree(context.Background(), SwitchRequest{}, &WorktreePlan{WorkspaceID: "new", TargetDir: "/new"})
	if Outcome(err) != CreationTakeover || repo.workspaces["new"].Cwd != "/new" || result.WorkspaceID != "new" || result.Effects.Workspace != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
func TestCreationGroupBindingKeepsActiveConversation(t *testing.T) {
	sess := &conversation.Session{Key: "session", ActiveThreadID: "old", ActiveTurnID: "turn", ActiveSubmissionID: "submission", Status: conversation.SessionStatusTurnInProgress.String()}
	repo := &configurationRepositoryStub{workspaces: map[string]domain.Workspace{}}
	state := &lifecycleRepositoryStub{state: LifecycleState{Sessions: []*conversation.Session{sess}}}
	svc := CreationService{Filesystem: creationFilesystem{}, Git: &creationGit{}, Lifecycle: &Lifecycle{Configuration: ConfigurationService{Repository: repo}, Repository: state}}
	result, err := svc.Clone(context.Background(), SwitchRequest{Session: sess, Binding: &routing.AgentBinding{ID: "bot"}}, "repo", &ClonePlan{WorkspaceID: "clone", TargetDir: "/clone", Worktree: &domain.CloneWorktreePlan{WorkspaceID: "new", TargetDir: "/new"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkspaceID != "new" || result.Effects.Binding.WorkspaceID != "new" || len(state.changes[0].Sessions) != 0 || sess.ActiveThreadID != "old" {
		t.Fatalf("result=%+v changes=%+v", result, state.changes)
	}
}
