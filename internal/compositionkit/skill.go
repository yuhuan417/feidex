package compositionkit

import (
	"context"
	"sync"

	configadapter "feidex/internal/adapter/config"
	skillapp "feidex/internal/application/skill"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
)

type SkillDependencies struct {
	Frontend identity.FrontendID
	Context  func() context.Context
	Config   *config.Config
	Mutex    *sync.RWMutex
	Sessions configadapter.SkillSessions
	Pending  skillapp.PendingRepository
	Catalog  skillapp.Catalog
}

func NewSkillService(deps SkillDependencies) *skillapp.Service {
	return &skillapp.Service{Deps: skillapp.Dependencies{
		Frontend: deps.Frontend, Context: deps.Context,
		Pending: deps.Pending, Catalog: deps.Catalog,
		Workspaces: configadapter.SkillWorkspaceRepository{
			Config: deps.Config, Mutex: deps.Mutex, Sessions: deps.Sessions,
		},
	}}
}
