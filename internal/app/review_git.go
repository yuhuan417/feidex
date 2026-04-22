package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"feidex/internal/config"
)

type reviewBranchOption struct {
	Name      string
	UpdatedAt int64
	Current   bool
	Default   bool
}

type reviewCommitOption struct {
	SHA      string
	ShortSHA string
	Date     string
	Subject  string
}

func (a *App) resolveReviewTarget(cwd string, target reviewTargetSpec) (reviewTargetSpec, error) {
	target.Type = strings.TrimSpace(target.Type)
	switch target.Type {
	case reviewTargetUncommitted:
		if _, err := a.gitRepoRoot(cwd); err != nil {
			return reviewTargetSpec{}, err
		}
		hasChanges, err := a.gitHasWorkingTreeChanges(cwd)
		if err != nil {
			return reviewTargetSpec{}, err
		}
		if !hasChanges {
			return reviewTargetSpec{}, fmt.Errorf("当前没有未提交改动")
		}
		return reviewTargetSpec{Type: reviewTargetUncommitted}, nil
	case reviewTargetBaseBranch:
		if _, err := a.gitRepoRoot(cwd); err != nil {
			return reviewTargetSpec{}, err
		}
		target.Branch = strings.TrimSpace(target.Branch)
		if target.Branch == "" {
			return reviewTargetSpec{}, fmt.Errorf("base branch 不能为空")
		}
		if err := a.gitVerifyCommitish(cwd, target.Branch); err != nil {
			return reviewTargetSpec{}, fmt.Errorf("branch 不存在或不可见")
		}
		hasWorkingChanges, err := a.gitHasWorkingTreeChanges(cwd)
		if err != nil {
			return reviewTargetSpec{}, err
		}
		hasCommittedDiff, err := a.gitHasDiffFromBase(cwd, target.Branch)
		if err != nil {
			return reviewTargetSpec{}, err
		}
		if !hasWorkingChanges && !hasCommittedDiff {
			return reviewTargetSpec{}, fmt.Errorf("当前 target 没有可审查差异")
		}
		return reviewTargetSpec{Type: reviewTargetBaseBranch, Branch: target.Branch}, nil
	case reviewTargetCommit:
		if _, err := a.gitRepoRoot(cwd); err != nil {
			return reviewTargetSpec{}, err
		}
		target.CommitSHA = strings.TrimSpace(target.CommitSHA)
		if target.CommitSHA == "" {
			return reviewTargetSpec{}, fmt.Errorf("commit 不能为空")
		}
		resolvedSHA, err := a.gitResolveCommitSHA(cwd, target.CommitSHA)
		if err != nil {
			return reviewTargetSpec{}, fmt.Errorf("commit 不存在或不唯一")
		}
		title := strings.TrimSpace(target.CommitTitle)
		if title == "" {
			title, _ = a.gitCommitTitle(cwd, resolvedSHA)
		}
		return reviewTargetSpec{Type: reviewTargetCommit, CommitSHA: resolvedSHA, CommitTitle: title}, nil
	case reviewTargetCustom:
		target.Instructions = strings.TrimSpace(target.Instructions)
		if target.Instructions == "" {
			return reviewTargetSpec{}, fmt.Errorf("review instructions 不能为空")
		}
		return reviewTargetSpec{Type: reviewTargetCustom, Instructions: target.Instructions}, nil
	default:
		return reviewTargetSpec{}, fmt.Errorf("unsupported review target %q", target.Type)
	}
}

func reviewBranchOptionLabel(option reviewBranchOption) string {
	prefix := ""
	switch {
	case option.Current && option.Default:
		prefix = "[当前][默认] "
	case option.Current:
		prefix = "[当前] "
	case option.Default:
		prefix = "[默认] "
	}
	return prefix + option.Name
}

func reviewCommitOptionLabel(option reviewCommitOption) string {
	parts := []string{option.ShortSHA}
	if strings.TrimSpace(option.Date) != "" {
		parts = append(parts, option.Date)
	}
	if strings.TrimSpace(option.Subject) != "" {
		parts = append(parts, option.Subject)
	}
	return strings.Join(parts, " | ")
}

func reviewBranchExists(options []reviewBranchOption, name string) bool {
	name = strings.TrimSpace(name)
	for _, option := range options {
		if option.Name == name {
			return true
		}
	}
	return false
}

func reviewCommitExists(options []reviewCommitOption, sha string) bool {
	sha = strings.TrimSpace(sha)
	for _, option := range options {
		if option.SHA == sha {
			return true
		}
	}
	return false
}

func (a *App) workspaceForSessionKey(sessionKey string) *config.Workspace {
	sess := a.appState().session(sessionKey)
	workspaceID := a.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = strings.TrimSpace(sess.WorkspaceID)
	}
	return config.FindWorkspace(a.cfg, workspaceID)
}

func (a *App) listReviewBranches(cwd string) ([]reviewBranchOption, error) {
	if _, err := a.gitRepoRoot(cwd); err != nil {
		return nil, err
	}
	output, err := a.gitOutput(cwd, "for-each-ref", "--sort=-committerdate", "--format=%(refname:short)"+gitFieldSep+"%(committerdate:unix)"+gitRecordSep, "refs/heads")
	if err != nil {
		return nil, err
	}
	currentBranch, _ := a.gitCurrentBranch(cwd)
	defaultBranch := ""
	if value, err := a.gitDefaultBranch(cwd); err == nil {
		defaultBranch = value
	}
	options := make([]reviewBranchOption, 0)
	for _, record := range parseGitStructuredOutput(output) {
		fields := strings.Split(record, gitFieldSep)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		if name == "" {
			continue
		}
		unixValue, _ := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
		options = append(options, reviewBranchOption{
			Name:      name,
			UpdatedAt: unixValue,
			Current:   name == currentBranch,
			Default:   name == defaultBranch,
		})
	}
	if defaultBranch != "" && !reviewBranchExists(options, defaultBranch) {
		options = append(options, reviewBranchOption{Name: defaultBranch, Default: true})
	}
	sort.SliceStable(options, func(i, j int) bool {
		left := options[i]
		right := options[j]
		switch {
		case left.Current != right.Current:
			return left.Current
		case left.Default != right.Default:
			return left.Default
		case left.UpdatedAt != right.UpdatedAt:
			return left.UpdatedAt > right.UpdatedAt
		default:
			return left.Name < right.Name
		}
	})
	return options, nil
}

func (a *App) listReviewCommits(cwd string, limit int) ([]reviewCommitOption, error) {
	if _, err := a.gitRepoRoot(cwd); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	output, err := a.gitOutput(cwd, "log", "--date=short", "--pretty=format:%H"+gitFieldSep+"%h"+gitFieldSep+"%cd"+gitFieldSep+"%s"+gitRecordSep, "-n", strconv.Itoa(limit))
	if err != nil {
		return nil, err
	}
	options := make([]reviewCommitOption, 0)
	for _, record := range parseGitStructuredOutput(output) {
		fields := strings.Split(record, gitFieldSep)
		if len(fields) < 4 {
			continue
		}
		sha := strings.TrimSpace(fields[0])
		if sha == "" {
			continue
		}
		options = append(options, reviewCommitOption{
			SHA:      sha,
			ShortSHA: strings.TrimSpace(fields[1]),
			Date:     strings.TrimSpace(fields[2]),
			Subject:  strings.TrimSpace(fields[3]),
		})
	}
	return options, nil
}

func parseGitStructuredOutput(output string) []string {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}
	raw := strings.Split(output, gitRecordSep)
	out := make([]string, 0, len(raw))
	for _, record := range raw {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		out = append(out, record)
	}
	return out
}

func (a *App) gitRepoRoot(cwd string) (string, error) {
	output, err := a.gitOutput(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("当前 workspace 不是 git 仓库")
	}
	return strings.TrimSpace(output), nil
}

func (a *App) gitCurrentBranch(cwd string) (string, error) {
	output, err := a.gitOutput(cwd, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (a *App) gitDefaultBranch(cwd string) (string, error) {
	output, err := a.gitOutput(cwd, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(output)
	branch = strings.TrimPrefix(branch, "origin/")
	return strings.TrimSpace(branch), nil
}

func (a *App) gitVerifyCommitish(cwd, ref string) error {
	_, err := a.gitOutput(cwd, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err
}

func (a *App) gitResolveCommitSHA(cwd, ref string) (string, error) {
	output, err := a.gitOutput(cwd, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (a *App) gitCommitTitle(cwd, sha string) (string, error) {
	output, err := a.gitOutput(cwd, "log", "-1", "--format=%s", sha)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (a *App) gitHasWorkingTreeChanges(cwd string) (bool, error) {
	output, err := a.gitOutput(cwd, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

func (a *App) gitHasDiffFromBase(cwd, branch string) (bool, error) {
	return a.gitCommandHasDiff(cwd, "diff", "--quiet", branch+"...HEAD", "--")
}

func (a *App) gitCommandHasDiff(cwd string, args ...string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return false, nil
	}
	var exitErr *exec.ExitError
	if strings.TrimSpace(string(output)) == "" && err == nil {
		return false, nil
	}
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return true, nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		message = err.Error()
	}
	return false, fmt.Errorf("git %s failed: %s", strings.Join(args, " "), message)
}

func (a *App) gitOutput(cwd string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("git %s failed: %s", strings.Join(args, " "), message)
	}
	return string(output), nil
}
