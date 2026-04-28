package app

import (
	"strings"

	appreview "feidex/internal/app/review"
	"feidex/internal/config"
)

type reviewGitService struct {
	app *App
}

func newReviewGitService(app *App) reviewGitService {
	return reviewGitService{app: app}
}

func (s reviewGitService) resolveReviewTarget(cwd string, target appreview.TargetSpec) (appreview.TargetSpec, error) {
	return appreview.NewGitService().ResolveTarget(cwd, target)
}

func (s reviewGitService) workspaceForSessionKey(sessionKey string) *config.Workspace {
	sess := s.app.State().Session(sessionKey)
	workspaceID := defaultWorkspaceID(s.app)
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

// Exported wrappers for sub-package interface satisfaction.
func (s reviewGitService) ReviewResolveTarget(cwd string, target appreview.TargetSpec) (appreview.TargetSpec, error) {
	return s.resolveReviewTarget(cwd, target)
}
func (s reviewGitService) ReviewListBranches(cwd string) ([]appreview.BranchOption, error) {
	return s.listReviewBranches(cwd)
}
func (s reviewGitService) ReviewListCommits(cwd string, limit int) ([]appreview.CommitOption, error) {
	return s.listReviewCommits(cwd, limit)
}
