package backend

import (
	"fmt"
	"strings"

	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type unsupportedDriver struct {
	rawKind string
}

type unsupportedConversationDriver struct {
	rawKind string
}

type unsupportedPermissionDriver struct {
	rawKind string
}

func unsupportedBackendUserMessage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "当前 frontend 还没有设置 backend，请先选择。"
	}
	return fmt.Sprintf("不支持的 backend: `%s`。", raw)
}

func unsupportedBackendError(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("backend not configured")
	}
	return fmt.Errorf("unsupported backend %q", raw)
}

func unsupportedBackendActionResponse(raw string) *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "warning", Content: unsupportedBackendUserMessage(raw)},
	}
}

func (d unsupportedDriver) Kind() string { return "" }

func (d unsupportedDriver) Capabilities() CapabilitySet {
	return CapabilitySet{
		Conversation: ConversationCapabilities{
			Noun:         "会话",
			SummaryLabel: "conversation",
		},
	}
}

func (d unsupportedDriver) Runtime() RuntimeDriver {
	return backendRuntimeDriver{displayName: "未设置", autoRetry: "自动重试"}
}

func (d unsupportedDriver) Conversation() ConversationDriver {
	return unsupportedConversationDriver{rawKind: d.rawKind}
}

func (d unsupportedDriver) Permission() PermissionDriver {
	return unsupportedPermissionDriver{rawKind: d.rawKind}
}

func (d unsupportedConversationDriver) PrimarySlash() string { return "" }

func (d unsupportedConversationDriver) Noun() string { return "会话" }

func (d unsupportedConversationDriver) SummaryLabel() string { return "conversation" }

func (d unsupportedConversationDriver) WorkspaceSwitchInFlightNotice() string {
	return "。当前 frontend 还没有设置 backend，请先选择。"
}

func (d unsupportedConversationDriver) WorkspaceSwitchBindingFailureNotice() string {
	return "。当前 frontend 还没有设置 backend，请先选择。"
}

func (d unsupportedConversationDriver) WorkspaceSwitchBindingNotice(*appworkspace.ThreadBinding) string {
	return "。当前 frontend 还没有设置 backend，请先选择。"
}

func (d unsupportedConversationDriver) EnsureWorkspaceThreadBinding(ops WorkspaceThreadOps, sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error) {
	return nil, unsupportedBackendError(d.rawKind)
}

func (d unsupportedConversationDriver) ListWorkspaceThreads(ops WorkspaceThreadOps, sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return nil, unsupportedBackendError(d.rawKind)
}

func (d unsupportedConversationDriver) StartWorkspaceThread(ops WorkspaceThreadOps, sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error) {
	return nil, unsupportedBackendError(d.rawKind)
}

func (d unsupportedPermissionDriver) SupportedScopes() []PermissionScope { return nil }

func (d unsupportedPermissionDriver) WorkspaceCommandUsage() string { return "/backend" }

func (d unsupportedPermissionDriver) AppendWorkspaceSummaryLines(app PermissionApp, lines []string, currentWS *config.Workspace) []string {
	return lines
}

func (d unsupportedPermissionDriver) WorkspaceConfigButtons(sessionKey string) []feishu.Button {
	return nil
}

func (d unsupportedPermissionDriver) AppendStatusLines(app PermissionApp, lines []string, sess *state.Session, ws *config.Workspace) []string {
	return lines
}

func (d unsupportedPermissionDriver) HandleWorkspaceCommand(req WorkspacePermissionCommandRequest) error {
	return unsupportedBackendError(d.rawKind)
}

func (d unsupportedPermissionDriver) RenderWorkspaceSandboxMenu(sessionKey string, deps WorkspacePermissionRenderDeps) (map[string]any, error) {
	return nil, unsupportedBackendError(d.rawKind)
}

func (d unsupportedPermissionDriver) RenderWorkspacePolicyMenu(sessionKey string, deps WorkspacePermissionRenderDeps) (map[string]any, error) {
	return nil, unsupportedBackendError(d.rawKind)
}

func (d unsupportedPermissionDriver) RenderWorkspacePermissionModeMenu(sessionKey string, deps WorkspacePermissionRenderDeps) (map[string]any, error) {
	return nil, unsupportedBackendError(d.rawKind)
}

func (d unsupportedPermissionDriver) CompleteWorkspaceSandboxSet(sessionKey, workspaceID, sandboxMode string, deps WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return unsupportedBackendActionResponse(d.rawKind), nil
}

func (d unsupportedPermissionDriver) CompleteWorkspacePolicySet(sessionKey, workspaceID, approvalPolicy string, deps WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return unsupportedBackendActionResponse(d.rawKind), nil
}

func (d unsupportedPermissionDriver) CompleteWorkspacePermissionModeSet(sessionKey, workspaceID, rawMode string, deps WorkspacePermissionModeUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return unsupportedBackendActionResponse(d.rawKind), nil
}

func (d unsupportedPermissionDriver) RenderWorkspaceMultiAgentMenu(sessionKey string, deps WorkspacePermissionRenderDeps) (map[string]any, error) {
	return nil, unsupportedBackendError(d.rawKind)
}

func (d unsupportedPermissionDriver) CompleteWorkspaceMultiAgentSet(sessionKey, workspaceID, mode string, deps WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return unsupportedBackendActionResponse(d.rawKind), nil
}

func (d unsupportedPermissionDriver) HandleConversationCommand(req ConversationPermissionCommandRequest) error {
	return unsupportedBackendError(d.rawKind)
}

func (d unsupportedPermissionDriver) RenderConversationSandboxMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error) {
	return nil, unsupportedBackendError(d.rawKind)
}

func (d unsupportedPermissionDriver) RenderConversationPolicyMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error) {
	return nil, unsupportedBackendError(d.rawKind)
}

func (d unsupportedPermissionDriver) RenderConversationPermissionModeMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error) {
	return nil, unsupportedBackendError(d.rawKind)
}

func (d unsupportedPermissionDriver) CompleteConversationSandboxSet(sessionKey, threadID, sandboxMode string, deps ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return unsupportedBackendActionResponse(d.rawKind), nil
}

func (d unsupportedPermissionDriver) CompleteConversationPolicySet(sessionKey, threadID, approvalPolicy string, deps ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return unsupportedBackendActionResponse(d.rawKind), nil
}

func (d unsupportedPermissionDriver) CompleteConversationPermissionModeSet(sessionKey, threadID, rawMode string, deps ConversationPermissionModeUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return unsupportedBackendActionResponse(d.rawKind), nil
}

func (d unsupportedPermissionDriver) RenderConversationMultiAgentMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error) {
	return nil, unsupportedBackendError(d.rawKind)
}

func (d unsupportedPermissionDriver) CompleteConversationMultiAgentSet(sessionKey, threadID, mode string, deps ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return unsupportedBackendActionResponse(d.rawKind), nil
}
