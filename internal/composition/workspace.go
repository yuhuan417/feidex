package composition

import (
	"sync"

	configadapter "feidex/internal/adapter/config"
	workspacecards "feidex/internal/adapter/feishu/workspace"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
	domain "feidex/internal/domain/workspace"
)

type WorkspacePresentationDependencies struct {
	Frontend   identity.FrontendID
	Config     *config.Config
	ConfigPath string
	Mutex      *sync.RWMutex
	Scopes     configadapter.WorkspaceViewScopes
	Backend    func() string
	PathPicker func(string, domain.PathPickerPayload) (map[string]any, error)
}

func NewWorkspacePresentation(deps WorkspacePresentationDependencies) *workspacecards.Presentation {
	return &workspacecards.Presentation{
		RenderService: &workspacecards.RenderService{PathPicker: deps.PathPicker},
		Views: workspaceapp.ViewService{Frontend: deps.Frontend, Repository: configadapter.WorkspaceViewRepository{
			Config: deps.Config, ConfigPath: deps.ConfigPath, Mutex: deps.Mutex, Scopes: deps.Scopes, Backend: deps.Backend,
		}},
	}
}
