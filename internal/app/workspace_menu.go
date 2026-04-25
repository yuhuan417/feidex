package app

import (
	"fmt"
	"strings"

	appworkspace "feidex/internal/app/workspace"
	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// workspaceConfigService wraps appworkspacecmd.ConfigService and provides
// lowercase method names for backward compatibility.
type workspaceConfigService struct {
	inner *appworkspacecmd.ConfigService
}

func newWorkspaceConfigService(app *App) workspaceConfigService {
	return workspaceConfigService{inner: newWorkspaceConfigServiceInner(app)}
}

type workspaceSettingOption = appworkspace.SettingOption

const workspaceCommandUsage = appworkspace.CommandUsage

var parseWorkspaceCloneArgs = appworkspace.ParseCloneArgs

var workspaceSandboxOptions = appworkspace.SandboxOptions

var workspaceApprovalPolicyOptions = appworkspace.ApprovalPolicyOptions

func (s workspaceConfigService) showWorkspaceMenu(msg *feishu.InboundMessage) error {
	return s.inner.ShowWorkspaceMenu(msg)
}

func (s workspaceConfigService) renderWorkspaceMenuCard(sessionKey string) map[string]any {
	return s.inner.RenderMenuCard(sessionKey)
}

func (s workspaceConfigService) currentWorkspaceForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace) {
	return s.inner.CurrentWorkspaceForMessage(msg)
}

// Exported wrapper for sub-package interface satisfaction.
func (s workspaceConfigService) CurrentThreadForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace, threadID string, err error) {
	return s.currentThreadForMessage(msg)
}

func (s workspaceConfigService) currentThreadForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace, threadID string, err error) {
	sessionKey, sess, ws = s.inner.CurrentWorkspaceForMessage(msg)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return sessionKey, sess, ws, "", fmt.Errorf("%s", primaryConversationMissingLabel(configuredBackend(s.inner.App)))
	}
	return sessionKey, sess, ws, strings.TrimSpace(sess.ActiveThreadID), nil
}

func (s workspaceConfigService) showWorkspaceSandboxMenu(msg *feishu.InboundMessage) error {
	return s.inner.ShowWorkspaceSandboxMenu(msg)
}

func (s workspaceConfigService) renderWorkspaceSandboxMenuCard(sessionKey string) (map[string]any, error) {
	return s.inner.RenderSandboxMenuCard(sessionKey)
}

func (s workspaceConfigService) showWorkspacePolicyMenu(msg *feishu.InboundMessage) error {
	return s.inner.ShowWorkspacePolicyMenu(msg)
}

func (s workspaceConfigService) renderWorkspacePolicyMenuCard(sessionKey string) (map[string]any, error) {
	return s.inner.RenderPolicyMenuCard(sessionKey)
}

// commandWorkspace dispatches the /workspace command.
func (s workspaceService) commandWorkspace(msg *feishu.InboundMessage, args []string) error {
	cfg := newWorkspaceConfigService(s.app)
	mgmt := newWorkspaceManagementService(s.app)
	return cfg.inner.CommandWorkspace(msg, args, mgmt.inner)
}
