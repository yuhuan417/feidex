package feishuapp

import (
	"context"
	codexadapter "feidex/internal/adapter/backend/codex"
	skillsadapter "feidex/internal/adapter/feishu/skills"
	appstate "feidex/internal/adapter/storage/json/scoped"
	appskill "feidex/internal/application/skill"
	"feidex/internal/compositionkit"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
	skillcatalog "feidex/internal/domain/skill"
	frontendruntime "feidex/internal/runtime"
	runtimeskill "feidex/internal/runtime/skill"
	"sync"
)

// This capability follows the frontend's client replacements without exposing
// a raw protocol client or its lifetime to the use case.
type skillsCatalog struct{ runtime runtimeView }

func (c skillsCatalog) ListSkills(ctx context.Context, cwd string, reload bool) (skillcatalog.SkillsListEntry, error) {
	client, err := c.runtime.requireCodexClient()
	if err != nil {
		return skillcatalog.SkillsListEntry{}, err
	}
	gateway := codexadapter.Gateway{Client: client}
	return gateway.ListSkills(ctx, cwd, reload)
}

// SkillUseCasePorts takes the values it needs instead of the frontend
// aggregate, so composition can supply them from what it already holds.
func SkillUseCasePorts(
	cfg *config.Config, mu *sync.RWMutex, contextFn func() context.Context,
	sessions *appstate.Store, pending *runtimeskill.Tracker,
	frontendID string, owner *frontendruntime.FrontendOwner,
) compositionkit.SkillDependencies {
	return compositionkit.SkillDependencies{
		Frontend: identity.FrontendID(frontendID),
		Context:  contextFn, Config: cfg, Mutex: mu,
		Sessions: sessions, Pending: pending,
		Catalog: skillsCatalog{runtime: runtimeView{owner: owner}},
	}
}

type SkillCommandInputs struct {
	Service      *appskill.Service
	FrontendID   string
	EffectRunner frontendruntime.EffectRunner
	Actors       *frontendruntime.SessionActors
	RunAsync     func(func()) bool
}

func BuildSkillCommands(inputs SkillCommandInputs) *skillsadapter.Service {
	return &skillsadapter.Service{
		Service: inputs.Service, Outbound: newEffectOutbound(inputs.FrontendID, inputs.EffectRunner),
		MakeSessionKey:       SessionKeyBuilder(inputs.FrontendID),
		ReplyInThreadEnabled: func(string) bool { return false },
		CommandLabel:         commandLabel,
		RunAsync: func(key string, work func()) bool {
			return inputs.RunAsync(func() {
				runSessionOnActor(inputs.Actors, key, work)
			})
		},
	}
}
