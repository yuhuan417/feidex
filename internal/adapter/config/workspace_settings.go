package config

import (
	"feidex/internal/adapter/storage/json/scoped"
	workspaceapp "feidex/internal/application/workspace"
	fileconfig "feidex/internal/config"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/routing"
	"feidex/internal/state"
	"fmt"
	"path/filepath"
	"strings"
)

type WorkspaceSettingsRepository struct {
	Source Source
	Scope  *appstate.Store
}

func (r WorkspaceSettingsRepository) UpdateSettings(key, id string, mutate func(*workspaceapp.SettingsRevision) error) error {
	r.Source.ConfigMu().Lock()
	defer r.Source.ConfigMu().Unlock()
	next := fileconfig.Clone(r.Source.Config())
	expected := r.Scope.StateStore().WorkspaceState()
	var profile *routing.BotProfile
	profileFrontend := strings.TrimSpace(r.Scope.FrontendID())
	if profileFrontend == "" {
		profileFrontend = "default"
	}
	for _, candidate := range expected.Profiles {
		if candidate != nil && candidate.FrontendID == profileFrontend {
			copy := *candidate
			profile = &copy
			break
		}
	}
	frontend, _, _, _, _ := identity.ParseSessionKey(key)
	if frontend != r.Scope.FrontendID() {
		return fmt.Errorf("session frontend does not match scope")
	}
	revision := workspaceapp.SettingsRevision{Workspace: fileconfig.FindWorkspace(next, id), Session: expected.Sessions[key], Profile: profile, AllowBypass: next.Claude.DangerouslySkipPermissions}
	if err := mutate(&revision); err != nil {
		return err
	}
	if err := next.Normalize(filepath.Dir(r.Source.ConfigPath())); err != nil {
		return err
	}
	var profiles []*routing.BotProfile
	if revision.Profile != nil {
		profiles = append(profiles, revision.Profile)
	}
	if err := r.Scope.StateStore().CommitWorkspace(state.WorkspaceMutation{Expected: expected, Profiles: profiles}, func() error { return fileconfig.Save(r.Source.ConfigPath(), next) }); err != nil {
		return err
	}
	*r.Source.Config() = *next
	return nil
}
