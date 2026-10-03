package workspace

import (
	"fmt"
	"strings"
)

const CommandUsage = "/workspace | /workspace list | /workspace new | /workspace new worktree [BRANCH] [ID] | /workspace clone GIT_URL [ID] [--parent DIR] | /workspace use ID | /workspace delete [ID] | /workspace sandbox [MODE] | /workspace policy [POLICY]"

// ParseCloneArgs parses /workspace clone arguments into repository URL,
// optional workspace ID, and optional parent directory.
func ParseCloneArgs(args []string) (repoURL, workspaceID, parentDir string, err error) {
	if len(args) < 2 || strings.TrimSpace(args[0]) != "clone" {
		return "", "", "", fmt.Errorf("usage: /workspace clone GIT_URL [ID] [--parent DIR]")
	}
	repoURL = strings.TrimSpace(args[1])
	if repoURL == "" {
		return "", "", "", fmt.Errorf("usage: /workspace clone GIT_URL [ID] [--parent DIR]")
	}
	switch len(args) {
	case 2:
		return repoURL, "", "", nil
	case 3:
		if strings.TrimSpace(args[2]) == "--parent" {
			return "", "", "", fmt.Errorf("usage: /workspace clone GIT_URL [ID] [--parent DIR]")
		}
		return repoURL, strings.TrimSpace(args[2]), "", nil
	case 4:
		if strings.TrimSpace(args[2]) != "--parent" || strings.TrimSpace(args[3]) == "" {
			return "", "", "", fmt.Errorf("usage: /workspace clone GIT_URL [ID] [--parent DIR]")
		}
		return repoURL, "", strings.TrimSpace(args[3]), nil
	case 5:
		if strings.TrimSpace(args[2]) == "" || strings.TrimSpace(args[3]) != "--parent" || strings.TrimSpace(args[4]) == "" {
			return "", "", "", fmt.Errorf("usage: /workspace clone GIT_URL [ID] [--parent DIR]")
		}
		return repoURL, strings.TrimSpace(args[2]), strings.TrimSpace(args[4]), nil
	default:
		return "", "", "", fmt.Errorf("usage: /workspace clone GIT_URL [ID] [--parent DIR]")
	}
}

// ParseWorktreeArgs parses /workspace new worktree arguments.
func ParseWorktreeArgs(args []string) (branchName, workspaceID string, err error) {
	if len(args) < 2 || strings.TrimSpace(args[0]) != "new" || strings.TrimSpace(args[1]) != "worktree" {
		return "", "", fmt.Errorf("usage: /workspace | /workspace list | /workspace new | /workspace new worktree [BRANCH] [ID]")
	}
	switch len(args) {
	case 2:
		return "", "", nil
	case 3:
		return strings.TrimSpace(args[2]), "", nil
	case 4:
		return strings.TrimSpace(args[2]), strings.TrimSpace(args[3]), nil
	default:
		return "", "", fmt.Errorf("usage: /workspace | /workspace list | /workspace new | /workspace new worktree [BRANCH] [ID]")
	}
}
