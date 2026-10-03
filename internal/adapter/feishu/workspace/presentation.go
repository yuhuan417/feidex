package workspace

import (
	"fmt"

	workspaceapp "feidex/internal/application/workspace"
)

// Views is the application query port used by workspace presentation entrypoints.
type Views interface {
	Snapshot(string) workspaceapp.View
	Settings(string, workspaceapp.Setting) (workspaceapp.SettingsView, error)
}

type Presentation struct {
	*RenderService
	Views Views
}

func (p *Presentation) RenderWorkspaceCloneCard(key, id string, payload ClonePayload) map[string]any {
	return p.RenderService.RenderWorkspaceCloneCard(p.Views.Snapshot(key), key, id, payload)
}
func (p *Presentation) RenderWorkspaceWorktreeCard(key, id string, payload WorktreePayload) map[string]any {
	return p.RenderService.RenderWorkspaceWorktreeCard(p.Views.Snapshot(key), key, id, payload)
}
func (p *Presentation) RenderWorkspaceMenuCard(key string) map[string]any {
	return p.RenderService.RenderWorkspaceMenuCard(p.Views.Snapshot(key), key)
}
func (p *Presentation) RenderWorkspaceChooseCard(key string) map[string]any {
	return p.RenderService.RenderWorkspaceChooseCard(p.Views.Snapshot(key), key)
}
func (p *Presentation) RenderWorkspaceDeleteMenuCard(key string) (map[string]any, error) {
	view := p.Views.Snapshot(key)
	if view.Group {
		return nil, fmt.Errorf("群聊不能移除本机 workspace 配置；请使用 /workspace unbind")
	}
	return p.RenderService.RenderWorkspaceDeleteMenuCard(view, key)
}
func (p *Presentation) RenderWorkspaceDeleteConfirmCard(key, id string) (map[string]any, error) {
	ws, err := p.Views.Snapshot(key).DeleteTarget(id)
	if err != nil {
		return nil, err
	}
	return p.RenderService.RenderWorkspaceDeleteConfirmCard(key, *ws)
}
func (p *Presentation) settings(key string, setting workspaceapp.Setting) (map[string]any, error) {
	view, err := p.Views.Settings(key, setting)
	if err != nil {
		return nil, err
	}
	return p.RenderService.RenderWorkspaceSettingsCard(key, view), nil
}
func (p *Presentation) RenderWorkspaceSandboxMenuCard(key string) (map[string]any, error) {
	return p.settings(key, workspaceapp.SettingSandbox)
}
func (p *Presentation) RenderWorkspacePolicyMenuCard(key string) (map[string]any, error) {
	return p.settings(key, workspaceapp.SettingPolicy)
}
func (p *Presentation) RenderWorkspaceMultiAgentMenuCard(key string) (map[string]any, error) {
	return p.settings(key, workspaceapp.SettingMultiAgent)
}
func (p *Presentation) RenderWorkspacePermissionModeMenuCard(key string) (map[string]any, error) {
	return p.settings(key, workspaceapp.SettingPermission)
}
