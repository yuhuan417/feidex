package review

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	TargetUncommitted = "uncommittedChanges"
	TargetBaseBranch  = "baseBranch"
	TargetCommit      = "commit"
	TargetCustom      = "custom"

	gitRecordSep = "\x1e"
	gitFieldSep  = "\x1f"
)

type TargetSpec struct {
	Type         string
	Branch       string
	CommitSHA    string
	CommitTitle  string
	Instructions string
}

type BranchOption struct {
	Name      string
	UpdatedAt int64
	Current   bool
	Default   bool
}

type CommitOption struct {
	SHA      string
	ShortSHA string
	Date     string
	Subject  string
}

type GitService struct{}

func NewGitService() GitService { return GitService{} }

func (s GitService) ResolveTarget(cwd string, target TargetSpec) (TargetSpec, error) {
	target.Type = strings.TrimSpace(target.Type)
	switch target.Type {
	case TargetUncommitted:
		if _, err := s.RepoRoot(cwd); err != nil {
			return TargetSpec{}, err
		}
		hasChanges, err := s.HasWorkingTreeChanges(cwd)
		if err != nil {
			return TargetSpec{}, err
		}
		if !hasChanges {
			return TargetSpec{}, fmt.Errorf("当前没有未提交改动")
		}
		return TargetSpec{Type: TargetUncommitted}, nil
	case TargetBaseBranch:
		if _, err := s.RepoRoot(cwd); err != nil {
			return TargetSpec{}, err
		}
		target.Branch = strings.TrimSpace(target.Branch)
		if target.Branch == "" {
			return TargetSpec{}, fmt.Errorf("base branch 不能为空")
		}
		if err := s.VerifyCommitish(cwd, target.Branch); err != nil {
			return TargetSpec{}, fmt.Errorf("branch 不存在或不可见")
		}
		hasWorkingChanges, err := s.HasWorkingTreeChanges(cwd)
		if err != nil {
			return TargetSpec{}, err
		}
		hasCommittedDiff, err := s.HasDiffFromBase(cwd, target.Branch)
		if err != nil {
			return TargetSpec{}, err
		}
		if !hasWorkingChanges && !hasCommittedDiff {
			return TargetSpec{}, fmt.Errorf("当前 target 没有可审查差异")
		}
		return TargetSpec{Type: TargetBaseBranch, Branch: target.Branch}, nil
	case TargetCommit:
		if _, err := s.RepoRoot(cwd); err != nil {
			return TargetSpec{}, err
		}
		target.CommitSHA = strings.TrimSpace(target.CommitSHA)
		if target.CommitSHA == "" {
			return TargetSpec{}, fmt.Errorf("commit 不能为空")
		}
		resolvedSHA, err := s.ResolveCommitSHA(cwd, target.CommitSHA)
		if err != nil {
			return TargetSpec{}, fmt.Errorf("commit 不存在或不唯一")
		}
		title := strings.TrimSpace(target.CommitTitle)
		if title == "" {
			title, _ = s.CommitTitle(cwd, resolvedSHA)
		}
		return TargetSpec{Type: TargetCommit, CommitSHA: resolvedSHA, CommitTitle: title}, nil
	case TargetCustom:
		target.Instructions = strings.TrimSpace(target.Instructions)
		if target.Instructions == "" {
			return TargetSpec{}, fmt.Errorf("review instructions 不能为空")
		}
		return TargetSpec{Type: TargetCustom, Instructions: target.Instructions}, nil
	default:
		return TargetSpec{}, fmt.Errorf("unsupported review target %q", target.Type)
	}
}

func BranchOptionLabel(option BranchOption) string {
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

func CommitOptionLabel(option CommitOption) string {
	parts := []string{option.ShortSHA}
	if strings.TrimSpace(option.Date) != "" {
		parts = append(parts, option.Date)
	}
	if strings.TrimSpace(option.Subject) != "" {
		parts = append(parts, option.Subject)
	}
	return strings.Join(parts, " | ")
}

func BranchExists(options []BranchOption, name string) bool {
	name = strings.TrimSpace(name)
	for _, option := range options {
		if option.Name == name {
			return true
		}
	}
	return false
}

func CommitExists(options []CommitOption, sha string) bool {
	sha = strings.TrimSpace(sha)
	for _, option := range options {
		if option.SHA == sha {
			return true
		}
	}
	return false
}

func (s GitService) ListBranches(cwd string) ([]BranchOption, error) {
	if _, err := s.RepoRoot(cwd); err != nil {
		return nil, err
	}
	output, err := s.Output(cwd, "for-each-ref", "--sort=-committerdate", "--format=%(refname:short)"+gitFieldSep+"%(committerdate:unix)"+gitRecordSep, "refs/heads")
	if err != nil {
		return nil, err
	}
	currentBranch, _ := s.CurrentBranch(cwd)
	defaultBranch := ""
	if value, err := s.DefaultBranch(cwd); err == nil {
		defaultBranch = value
	}
	options := make([]BranchOption, 0)
	for _, record := range ParseStructuredOutput(output) {
		fields := strings.Split(record, gitFieldSep)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		if name == "" {
			continue
		}
		unixValue, _ := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
		options = append(options, BranchOption{
			Name:      name,
			UpdatedAt: unixValue,
			Current:   name == currentBranch,
			Default:   name == defaultBranch,
		})
	}
	if defaultBranch != "" && !BranchExists(options, defaultBranch) {
		options = append(options, BranchOption{Name: defaultBranch, Default: true})
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

func (s GitService) ListCommits(cwd string, limit int) ([]CommitOption, error) {
	if _, err := s.RepoRoot(cwd); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	output, err := s.Output(cwd, "log", "--date=short", "--pretty=format:%H"+gitFieldSep+"%h"+gitFieldSep+"%cd"+gitFieldSep+"%s"+gitRecordSep, "-n", strconv.Itoa(limit))
	if err != nil {
		return nil, err
	}
	options := make([]CommitOption, 0)
	for _, record := range ParseStructuredOutput(output) {
		fields := strings.Split(record, gitFieldSep)
		if len(fields) < 4 {
			continue
		}
		sha := strings.TrimSpace(fields[0])
		if sha == "" {
			continue
		}
		options = append(options, CommitOption{
			SHA:      sha,
			ShortSHA: strings.TrimSpace(fields[1]),
			Date:     strings.TrimSpace(fields[2]),
			Subject:  strings.TrimSpace(fields[3]),
		})
	}
	return options, nil
}

func ParseStructuredOutput(output string) []string {
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

func (s GitService) RepoRoot(cwd string) (string, error) {
	output, err := s.Output(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("当前 workspace 不是 git 仓库")
	}
	return strings.TrimSpace(output), nil
}

func (s GitService) CurrentBranch(cwd string) (string, error) {
	output, err := s.Output(cwd, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (s GitService) DefaultBranch(cwd string) (string, error) {
	output, err := s.Output(cwd, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(output)
	branch = strings.TrimPrefix(branch, "origin/")
	return strings.TrimSpace(branch), nil
}

func (s GitService) VerifyCommitish(cwd, ref string) error {
	_, err := s.Output(cwd, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err
}

func (s GitService) ResolveCommitSHA(cwd, ref string) (string, error) {
	output, err := s.Output(cwd, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (s GitService) CommitTitle(cwd, sha string) (string, error) {
	output, err := s.Output(cwd, "log", "-1", "--format=%s", sha)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (s GitService) HasWorkingTreeChanges(cwd string) (bool, error) {
	output, err := s.Output(cwd, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

func (s GitService) HasDiffFromBase(cwd, branch string) (bool, error) {
	return s.CommandHasDiff(cwd, "diff", "--quiet", branch+"...HEAD", "--")
}

func (s GitService) CommandHasDiff(cwd string, args ...string) (bool, error) {
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

func (s GitService) Output(cwd string, args ...string) (string, error) {
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
