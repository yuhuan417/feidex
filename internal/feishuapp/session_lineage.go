package feishuapp

import (
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
)

func effectiveThreadApprovalPolicy(sess *conversation.Session, ws *config.Workspace) string {
	workspaceValue := ""
	if ws != nil {
		workspaceValue = ws.ApprovalPolicy
	}
	return conversation.EffectiveApprovalPolicy(sess, workspaceValue)
}

func effectiveThreadSandboxMode(sess *conversation.Session, ws *config.Workspace) string {
	workspaceValue := ""
	if ws != nil {
		workspaceValue = ws.SandboxMode
	}
	return conversation.EffectiveSandboxMode(sess, workspaceValue)
}

func sessionCanResumeThreadForSubmission(sess *conversation.Session, sub *domainsubmission.Submission) bool {
	return (sub != nil && conversation.CanResumeThreadForWorkspace(sess, sub.WorkspaceID))
}
