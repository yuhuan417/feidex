package app

import (
	"strings"

	appreview "feidex/internal/app/review"
	"feidex/internal/config"
)

func (s reviewGitService) resolveReviewTarget(cwd string, target appreview.TargetSpec) (appreview.TargetSpec, error) {
	return appreview.NewGitService().ResolveTarget(cwd, target)
}

func (s reviewGitService) workspaceForSessionKey(sessionKey string) *config.Workspace {
	sess := s.app.appState().session(sessionKey)
	workspaceID := s.app.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = strings.TrimSpace(sess.WorkspaceID)
	}
	return config.FindWorkspace(s.app.cfg, workspaceID)
}

func (s reviewGitService) listReviewBranches(cwd string) ([]appreview.BranchOption, error) {
	return appreview.NewGitService().ListBranches(cwd)
}

func (s reviewGitService) listReviewCommits(cwd string, limit int) ([]appreview.CommitOption, error) {
	return appreview.NewGitService().ListCommits(cwd, limit)
}
