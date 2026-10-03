package composition

import (
	"sync"

	configadapter "feidex/internal/adapter/config"
	pickercards "feidex/internal/adapter/feishu/pathpicker"
	workspacecards "feidex/internal/adapter/feishu/workspace"
	pickerfs "feidex/internal/adapter/filesystem/pathpicker"
	pickerapp "feidex/internal/application/pathpicker"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
)

type WorkspacePresentationDependencies struct {
	Frontend   identity.FrontendID
	Config     *config.Config
	ConfigPath string
	Mutex      *sync.RWMutex
	Scopes     configadapter.WorkspaceViewScopes
	Backend    func() string
}

func NewWorkspacePresentation(deps WorkspacePresentationDependencies) *workspacecards.Presentation {
	return &workspacecards.Presentation{
		RenderService: &workspacecards.RenderService{},
		Picker:        pickercards.Presentation{Views: pickerapp.Service{Filesystem: pickerfs.Filesystem{}}},
		Views: workspaceapp.ViewService{Frontend: deps.Frontend, Repository: configadapter.WorkspaceViewRepository{
			Config: deps.Config, ConfigPath: deps.ConfigPath, Mutex: deps.Mutex, Scopes: deps.Scopes, Backend: deps.Backend,
		}},
	}
}
