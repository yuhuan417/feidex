package feishuapp

import (
	"context"
	"os"

	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	workspaceapp "feidex/internal/application/workspace"
)

type workspaceFilesystem struct{ cfgPath string }

func (f workspaceFilesystem) ResolvePath(path string) string {
	return resolveConfigRelativePath(f.cfgPath, path)
}
func (f workspaceFilesystem) MakeDirectory(path string) error { return os.MkdirAll(path, 0o755) }

type workspaceGit struct{}

func (workspaceGit) Clone(ctx context.Context, repo, target string, report func(string)) error {
	return workspaceGitClone(ctx, repo, target, report)
}
func (workspaceGit) Worktree(ctx context.Context, repo, branch, target string) error {
	return appworkspacecmd.GitWorktreeAdd(ctx, repo, branch, target)
}

// WorkspaceCreationPorts needs only the config path to resolve relative
// targets, so it takes that rather than the frontend aggregate.
func WorkspaceCreationPorts(cfgPath string) (workspaceapp.CreationFilesystem, workspaceapp.CreationGit) {
	return workspaceFilesystem{cfgPath: cfgPath}, workspaceGit{}
}
