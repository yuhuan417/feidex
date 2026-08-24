package app

import (
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
			if isGroupSessionKey(sessionKey) {
				if binding := bindingForSessionKey(a, sessionKey); binding != nil {
					return strings.TrimSpace(binding.WorkspaceID)
				}
				return ""
			}
			return appcore.ResolveWorkspaceSelectionForSession(a, sess)
		},
	})
}
