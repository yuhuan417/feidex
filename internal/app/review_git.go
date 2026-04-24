package app

import (
	"strings"

	appreview "feidex/internal/app/review"
	"feidex/internal/config"
)

type reviewBranchOption = appreview.BranchOption
type reviewCommitOption = appreview.CommitOption

func (s reviewGitService) resolveReviewTarget(cwd string, target reviewTargetSpec) (reviewTargetSpec, error) {
	return appreview.NewGitService().ResolveTarget(cwd, target)
}

func reviewBranchOptionLabel(option reviewBranchOption) string {
	return appreview.BranchOptionLabel(option)
}

func reviewCommitOptionLabel(option reviewCommitOption) string {
	return appreview.CommitOptionLabel(option)
}

func reviewBranchExists(options []reviewBranchOption, name string) bool {
	return appreview.BranchExists(options, name)
}

func reviewCommitExists(options []reviewCommitOption, sha string) bool {
	return appreview.CommitExists(options, sha)
}

func (s reviewGitService) workspaceForSessionKey(sessionKey string) *config.Workspace {
	sess := s.app.appState().session(sessionKey)
	workspaceID := s.app.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = strings.TrimSpace(sess.WorkspaceID)
	}
	return config.FindWorkspace(s.app.cfg, workspaceID)
}

func (s reviewGitService) listReviewBranches(cwd string) ([]reviewBranchOption, error) {
	return appreview.NewGitService().ListBranches(cwd)
}

func (s reviewGitService) listReviewCommits(cwd string, limit int) ([]reviewCommitOption, error) {
	return appreview.NewGitService().ListCommits(cwd, limit)
}

func parseGitStructuredOutput(output string) []string {
	return appreview.ParseStructuredOutput(output)
}

func (s reviewGitService) gitRepoRoot(cwd string) (string, error) {
	return appreview.NewGitService().RepoRoot(cwd)
}

func (s reviewGitService) gitCurrentBranch(cwd string) (string, error) {
	return appreview.NewGitService().CurrentBranch(cwd)
}

func (s reviewGitService) gitDefaultBranch(cwd string) (string, error) {
	return appreview.NewGitService().DefaultBranch(cwd)
}

func (s reviewGitService) gitVerifyCommitish(cwd, ref string) error {
	return appreview.NewGitService().VerifyCommitish(cwd, ref)
}

func (s reviewGitService) gitResolveCommitSHA(cwd, ref string) (string, error) {
	return appreview.NewGitService().ResolveCommitSHA(cwd, ref)
}

func (s reviewGitService) gitCommitTitle(cwd, sha string) (string, error) {
	return appreview.NewGitService().CommitTitle(cwd, sha)
}

func (s reviewGitService) gitHasWorkingTreeChanges(cwd string) (bool, error) {
	return appreview.NewGitService().HasWorkingTreeChanges(cwd)
}

func (s reviewGitService) gitHasDiffFromBase(cwd, branch string) (bool, error) {
	return appreview.NewGitService().HasDiffFromBase(cwd, branch)
}

func (s reviewGitService) gitCommandHasDiff(cwd string, args ...string) (bool, error) {
	return appreview.NewGitService().CommandHasDiff(cwd, args...)
}

func (s reviewGitService) gitOutput(cwd string, args ...string) (string, error) {
	return appreview.NewGitService().Output(cwd, args...)
}
