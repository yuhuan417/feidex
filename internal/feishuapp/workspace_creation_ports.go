package feishuapp

import (
	"context"
	"os"

	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	workspaceapp "feidex/internal/application/workspace"
)

type workspaceFilesystem struct{ app *App }

func (f workspaceFilesystem) ResolvePath(path string) string {
	return resolveConfigRelativePath(f.app, path)
}
func (f workspaceFilesystem) MakeDirectory(path string) error { return os.MkdirAll(path, 0o755) }

type workspaceGit struct{}

func (workspaceGit) Clone(ctx context.Context, repo, target string, report func(string)) error {
	return workspaceGitClone(ctx, repo, target, report)
}
func (workspaceGit) Worktree(ctx context.Context, repo, branch, target string) error {
	return appworkspacecmd.GitWorktreeAdd(ctx, repo, branch, target)
}

func WorkspaceCreationPorts(a *App) (workspaceapp.CreationFilesystem, workspaceapp.CreationGit) {
	return workspaceFilesystem{app: a}, workspaceGit{}
}
