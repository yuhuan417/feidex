package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"feidex/internal/app/appcore"
	apppathpick "feidex/internal/app/pathpick"
	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/state"
)

func newWorkspaceRenderServiceInner(a *App) *appworkspacecmd.RenderService {
	bcfg := newBackendConfigurationService(a)
	return appworkspacecmd.NewRenderService(appworkspacecmd.RenderDeps{
		App: a,
		State: appworkspacecmd.StateDeps{
			GetSession: func(key string) *state.Session { return a.State().Session(key) },
		},
		Backend: appworkspacecmd.BackendConfigDeps{
			BackendWorkspaceSummaryLines:  bcfg.appendBackendWorkspaceSummaryLines,
			BackendWorkspaceConfigButtons: bcfg.backendWorkspaceConfigButtons,
		},
		Formatting: appworkspacecmd.FormattingDeps{
			FormatMenuBody: menuCardBody,
		},
		PathPicker: appworkspacecmd.PathPickerDeps{
			RenderPathPickerCard: func(requestID string, payload appworkspacecmd.PathPickerPayload) (map[string]any, error) {
				return apppathpick.RenderCard(requestID, payload)
			},
		},
		Management: appworkspacecmd.RenderManagementDeps{
			DefaultWorkspaceCloneRoot: func(*config.Workspace) string { return "/" },
			DefaultWorkspaceCloneParent: func(ws *config.Workspace) string {
				if ws != nil && strings.TrimSpace(ws.Cwd) != "" {
					return filepath.Dir(strings.TrimSpace(ws.Cwd))
				}
				if cp := strings.TrimSpace(a.ConfigPath()); cp != "" {
					return filepath.Dir(cp)
				}
				return "."
			},
		},
		WorkspaceIDForSession: func(sessionKey string, sess *state.Session) string {
			if groupBindingSessionScopeActive(a, sessionKey) {
				if binding := bindingForSessionKey(a, sessionKey); binding != nil {
					return strings.TrimSpace(binding.WorkspaceID)
				}
				return ""
			}
			return appcore.ResolveWorkspaceSelectionForSession(a, sess)
		},
		WorkspaceMenuBodyLines: func(sessionKey string, sess *state.Session, lines []string) []string {
			if !groupBindingSessionScopeActive(a, sessionKey) {
				return lines
			}
			binding := bindingForSessionKey(a, sessionKey)
			if binding == nil || strings.TrimSpace(binding.WorkspaceID) == "" {
				lines = append(lines, "当前 Bot 在本群还没有配置工作区。")
			}
			if binding != nil {
				pending := binding.PendingMessage
				pendingCount := len(binding.PendingMessages)
				if pendingCount > 0 {
					pending = binding.PendingMessages[0]
				}
				if preview := pendingBindingMessagePreview(pending); preview != "" {
					if pendingCount > 0 {
						lines = append(lines, fmt.Sprintf("已暂存原消息 pending queue `%d`，下一条: `%s`", pendingCount, preview))
					} else {
						lines = append(lines, "已暂存原消息，配置工作区后会继续处理: `"+preview+"`")
					}
				}
			}
			return lines
		},
	})
}

func pendingBindingMessagePreview(pending *state.AgentBindingPendingMessage) string {
	if pending == nil {
		return ""
	}
	preview := truncate(strings.TrimSpace(pending.Text), 80)
	if preview == "" && len(pending.Attachments) > 0 {
		preview = fmt.Sprintf("%d 个附件", len(pending.Attachments))
	}
	if preview == "" {
		preview = strings.TrimSpace(pending.MessageID)
	}
	return preview
}
