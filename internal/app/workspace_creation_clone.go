package app

import (
	"context"

	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/feishu"
)

const workspaceCloneProgressKeepLines = appworkspace.CloneProgressKeepLines
const workspaceClonePatchInterval = appworkspace.ClonePatchInterval

type workspaceCloneProgressReporter = appworkspace.CloneProgressReporter
type workspaceCloneTracker = appworkspace.CloneTracker
type workspaceCloneOperation = appworkspace.CloneOperation

var workspaceGitClone = appworkspace.GitClone

func newWorkspaceCloneTracker() *workspaceCloneTracker {
	return appworkspace.NewCloneTracker()
}

func newWorkspaceCloneOperation(cancel context.CancelFunc) *workspaceCloneOperation {
	return appworkspace.NewCloneOperation(cancel)
}

var readWorkspaceCloneOutput = appworkspace.ReadCloneOutput

// Clone tracker accessors — delegate to the adapter closures via the
// ManagementService callbacks. These thin wrappers preserve the original
// method signatures on workspaceManagementService.

func (s workspaceManagementService) setWorkspaceCloneOperation(requestID string, op *workspaceCloneOperation) {
	s.inner.SetWorkspaceCloneOperation(requestID, op)
}

func (s workspaceManagementService) workspaceCloneOperation(requestID string) *workspaceCloneOperation {
	return s.inner.GetWorkspaceCloneOperation(requestID)
}

func (s workspaceManagementService) clearWorkspaceCloneOperation(requestID string) {
	s.inner.ClearWorkspaceCloneOperation(requestID)
}

func (s workspaceManagementService) finishWorkspaceCloneSubmit(ctx context.Context, op *workspaceCloneOperation, requestID, messageID, sessionKey, userID, chatID, chatType, parentDir string, payload workspaceClonePayload) {
	s.inner.FinishWorkspaceCloneSubmit(ctx, op, requestID, messageID, sessionKey, userID, chatID, chatType, parentDir, payload)
}

func (s workspaceManagementService) prepareWorkspaceClone(repoURL, explicitID, parentDir string) (*workspaceClonePlan, error) {
	return s.inner.PrepareWorkspaceClone(repoURL, explicitID, parentDir)
}

func (s workspaceManagementService) cloneWorkspaceInParent(ctx context.Context, sessionKey, userID, chatID, chatType, repoURL, explicitID, parentDir string, report workspaceCloneProgressReporter) (string, string, error) {
	return s.inner.CloneWorkspaceInParent(ctx, sessionKey, userID, chatID, chatType, repoURL, explicitID, parentDir, report)
}

func (s workspaceManagementService) cloneWorkspaceAndSwitch(msg *feishu.InboundMessage, repoURL, explicitID string) error {
	return s.inner.CloneWorkspaceAndSwitch(msg, repoURL, explicitID)
}

func (s workspaceManagementService) cloneWorkspaceAndSwitchInSelectedParent(msg *feishu.InboundMessage, repoURL, explicitID, parentDir string) error {
	return s.inner.CloneWorkspaceAndSwitchInSelectedParent(msg, repoURL, explicitID, parentDir)
}
