package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GitWorktreeAdd creates a new worktree from baseRepoRoot at targetDir using a
// new local branch. It is intentionally non-interactive for remote Feishu flows.
var GitWorktreeAdd = func(ctx context.Context, baseRepoRoot, branchName, targetDir string) error {
	baseRepoRoot = strings.TrimSpace(baseRepoRoot)
	branchName = strings.TrimSpace(branchName)
	targetDir = strings.TrimSpace(targetDir)
	if baseRepoRoot == "" || branchName == "" || targetDir == "" {
		return fmt.Errorf("base repo, branch, and target dir are required")
	}
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "-b", branchName, targetDir, "HEAD")
	cmd.Dir = baseRepoRoot
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(strings.Join([]string{stdout.String(), stderr.String()}, "\n"))
		if output == "" {
			output = err.Error()
		}
		return fmt.Errorf("git worktree add failed: %s", output)
	}
	return nil
}
