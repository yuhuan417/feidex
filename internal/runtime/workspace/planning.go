package workspace

import (
	"errors"
	workspaceapp "feidex/internal/application/workspace"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type PlanningFilesystem struct{}

func (PlanningFilesystem) Stat(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, workspaceapp.ErrPathNotExist
	}
	return err == nil, err
}

type PlanningGit struct{}

func (PlanningGit) Root(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", fmt.Errorf("cwd is required")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("未检测到 git: %w", err)
	}
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("git repo root is empty")
	}
	return filepath.Clean(root), nil
}

func (PlanningGit) ValidateBranch(branchName string) error {
	branchName = strings.TrimSpace(branchName)
	if branchName == "" {
		return fmt.Errorf("branch name is required")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return err
	}
	cmd := exec.Command("git", "check-ref-format", "--branch", branchName)
	if output, err := cmd.CombinedOutput(); err != nil {
		if text := strings.TrimSpace(string(output)); text != "" {
			return fmt.Errorf("%s", text)
		}
		return err
	}
	return nil
}

func (PlanningGit) BranchExists(repoRoot, branchName string) (bool, error) {
	cmd := exec.Command("git", "-C", strings.TrimSpace(repoRoot), "show-ref", "--verify", "--quiet", "refs/heads/"+strings.TrimSpace(branchName))
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
