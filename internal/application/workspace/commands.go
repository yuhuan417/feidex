package workspace

import (
	"context"
	"feidex/internal/domain/conversation"
	domain "feidex/internal/domain/workspace"
	"fmt"
	"strings"
)

const CommandUsage = "/workspace | /workspace list | /workspace new | /workspace new worktree [BRANCH] [ID] | /workspace clone GIT_URL [ID] [--parent DIR] | /workspace use ID | /workspace delete [ID] | /workspace sandbox [MODE] | /workspace policy [POLICY]"

func ParseCloneArgs(args []string) (repoURL, workspaceID, parentDir string, err error) {
	if len(args) < 2 || strings.TrimSpace(args[0]) != "clone" {
		return "", "", "", fmt.Errorf("usage: %s", CommandUsage)
	}
	repoURL = strings.TrimSpace(args[1])
	if repoURL == "" {
		return "", "", "", fmt.Errorf("usage: %s", CommandUsage)
	}
	switch len(args) {
	case 2:
		return repoURL, "", "", nil
	case 3:
		if strings.TrimSpace(args[2]) == "--parent" {
			return "", "", "", fmt.Errorf("usage: %s", CommandUsage)
		}
		return repoURL, strings.TrimSpace(args[2]), "", nil
	case 4:
		if strings.TrimSpace(args[2]) != "--parent" || strings.TrimSpace(args[3]) == "" {
			return "", "", "", fmt.Errorf("usage: %s", CommandUsage)
		}
		return repoURL, "", strings.TrimSpace(args[3]), nil
	case 5:
		if strings.TrimSpace(args[2]) == "" || strings.TrimSpace(args[3]) != "--parent" || strings.TrimSpace(args[4]) == "" {
			return "", "", "", fmt.Errorf("usage: %s", CommandUsage)
		}
		return repoURL, strings.TrimSpace(args[2]), strings.TrimSpace(args[4]), nil
	default:
		return "", "", "", fmt.Errorf("usage: %s", CommandUsage)
	}
}

func ParseWorktreeArgs(args []string) (branchName, workspaceID string, err error) {
	if len(args) < 2 || strings.TrimSpace(args[0]) != "new" || strings.TrimSpace(args[1]) != "worktree" {
		return "", "", fmt.Errorf("usage: %s", CommandUsage)
	}
	switch len(args) {
	case 2:
		return "", "", nil
	case 3:
		return strings.TrimSpace(args[2]), "", nil
	case 4:
		return strings.TrimSpace(args[2]), strings.TrimSpace(args[3]), nil
	default:
		return "", "", fmt.Errorf("usage: %s", CommandUsage)
	}
}

type SwitchOutcome struct {
	Workspace    *domain.Workspace
	Session      *conversation.Session
	Binding      *conversation.ThreadBinding
	BindingError error
}

func (w Workflow) Switch(req SwitchRequest, async bool) (SwitchOutcome, error) {
	effects, err := w.Creation.Lifecycle.Switch(req)
	if err != nil {
		return SwitchOutcome{}, err
	}
	out := SwitchOutcome{Workspace: effects.Workspace, Session: effects.Session}
	if async {
		w.Effects.Apply(effects)
	} else {
		for _, key := range effects.ClearLiveThreads {
			w.Effects.Runtime.ClearLive(key)
		}
		if effects.Session != nil && effects.Workspace != nil {
			out.Binding, out.BindingError = w.Effects.Conversations.EnsureWorkspaceThreadBinding(effects.Session.Key, effects.Session, effects.Workspace)
		}
	}
	return out, nil
}

func (w Workflow) ValidateDeletion(key, id string) error {
	return w.Creation.Lifecycle.ValidateDeletion(key, id)
}

func (w Workflow) Delete(key, id string) error {
	effects, err := w.Creation.Lifecycle.Delete(key, id)
	if err == nil {
		w.Effects.Apply(effects)
	}
	return err
}

func (w Workflow) Create(req SwitchRequest, id, name, cwd string) (CreationResult, error) {
	result, err := w.Creation.CreateAndSwitch(req, id, name, cwd)
	if err == nil {
		w.Effects.Apply(result.Effects)
	}
	return result, err
}

func (w Workflow) Clone(ctx context.Context, req SwitchRequest, payload ClonePayload, parent string, report func(string)) (CreationResult, error) {
	plan, err := w.Planning.PrepareWorkspaceClonePayload(payload, parent)
	if err != nil {
		return CreationResult{}, err
	}
	result, err := w.Creation.Clone(ctx, req, payload.RepoURL, plan, report)
	if err == nil {
		w.Effects.Apply(result.Effects)
	}
	return result, err
}
