package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	domain "feidex/internal/domain/workspace"
)

type CreationFilesystem interface {
	ResolvePath(string) string
	MakeDirectory(string) error
}

type CreationGit interface {
	Clone(context.Context, string, string, func(string)) error
	Worktree(context.Context, string, string, string) error
}

type CreationService struct {
	Filesystem CreationFilesystem
	Git        CreationGit
	Lifecycle  *Lifecycle
}

type CreationResult struct {
	WorkspaceID, TargetDir string
	Effects                LifecycleEffects
}

func (s CreationService) CreateLocal(id, name, cwd string) (*domain.Workspace, error) {
	id, cwd = strings.TrimSpace(id), strings.TrimSpace(cwd)
	if id == "" {
		return nil, fmt.Errorf("workspace_id is required")
	}
	if cwd == "" {
		return nil, fmt.Errorf("cwd is required")
	}
	cwd = s.Filesystem.ResolvePath(cwd)
	if err := s.Filesystem.MakeDirectory(cwd); err != nil {
		return nil, err
	}
	return s.Lifecycle.Configuration.Create(domain.Workspace{
		ID: id, Name: firstNonEmpty(name, id), Cwd: cwd,
		ApprovalPolicy: "never", SandboxMode: "danger-full-access", MultiAgentMode: "explicitRequestOnly",
	})
}

func (s CreationService) CreateAndSwitch(req SwitchRequest, id, name, cwd string) (CreationResult, error) {
	if req.Binding == nil {
		if reason := SwitchBlockedReason(req.Session, false); reason != "" {
			return CreationResult{}, fmt.Errorf("%s", reason)
		}
	}
	ws, err := s.CreateLocal(id, name, cwd)
	if err != nil {
		return CreationResult{}, err
	}
	result := CreationResult{WorkspaceID: ws.ID, TargetDir: ws.Cwd}
	req.WorkspaceID = ws.ID
	result.Effects, err = s.Lifecycle.Switch(req)
	return result, err
}

func (s CreationService) Clone(ctx context.Context, req SwitchRequest, repoURL string, plan *ClonePlan, report func(string)) (CreationResult, error) {
	if plan == nil {
		return CreationResult{}, fmt.Errorf("clone 创建参数无效，请重新发起。")
	}
	if err := s.Filesystem.MakeDirectory(filepath.Dir(plan.TargetDir)); err != nil {
		return CreationResult{}, err
	}
	if err := s.Git.Clone(ctx, strings.TrimSpace(repoURL), plan.TargetDir, report); err != nil {
		return CreationResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return CreationResult{}, err
	}
	id, target := plan.WorkspaceID, plan.TargetDir
	if worktree := plan.Worktree; worktree != nil {
		if err := s.Git.Worktree(ctx, worktree.BaseRepoRoot, worktree.BranchName, worktree.TargetDir); err != nil {
			return CreationResult{}, err
		}
		if err := ctx.Err(); err != nil {
			return CreationResult{}, err
		}
		id, target = worktree.WorkspaceID, worktree.TargetDir
	}
	return s.complete(req, id, target)
}

func (s CreationService) Worktree(ctx context.Context, req SwitchRequest, plan *WorktreePlan) (CreationResult, error) {
	if plan == nil {
		return CreationResult{}, fmt.Errorf("worktree 创建参数无效，请重新发起。")
	}
	if err := s.Filesystem.MakeDirectory(filepath.Dir(plan.TargetDir)); err != nil {
		return CreationResult{}, err
	}
	if err := s.Git.Worktree(ctx, plan.BaseRepoRoot, plan.BranchName, plan.TargetDir); err != nil {
		return CreationResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return CreationResult{}, err
	}
	return s.complete(req, plan.WorkspaceID, plan.TargetDir)
}

func (s CreationService) complete(req SwitchRequest, id, target string) (CreationResult, error) {
	result := CreationResult{WorkspaceID: id, TargetDir: target}
	if _, err := s.CreateLocal(id, id, target); err != nil {
		return result, &CloneTakeoverError{WorkspaceID: id, TargetDir: target, Err: err}
	}
	req.WorkspaceID = id
	effects, err := s.Lifecycle.Switch(req)
	if err != nil {
		return result, &CloneTakeoverError{WorkspaceID: id, TargetDir: target, Err: err}
	}
	result.Effects = effects
	return result, nil
}

type CreationOutcome string

const (
	CreationCompleted CreationOutcome = "completed"
	CreationCancelled CreationOutcome = "cancelled"
	CreationFailed    CreationOutcome = "failed"
	CreationTakeover  CreationOutcome = "takeover"
)

func Outcome(err error) CreationOutcome {
	if err == nil {
		return CreationCompleted
	}
	if errors.Is(err, context.Canceled) {
		return CreationCancelled
	}
	var takeover *CloneTakeoverError
	if errors.As(err, &takeover) {
		return CreationTakeover
	}
	return CreationFailed
}
