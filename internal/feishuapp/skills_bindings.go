package feishuapp

import (
	"context"
	codexadapter "feidex/internal/adapter/backend/codex"
	skillsadapter "feidex/internal/adapter/feishu/skills"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/compositionkit"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
	skillcatalog "feidex/internal/domain/skill"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	runtimeskill "feidex/internal/runtime/skill"
	"sync"
)

type skillsOutbound struct{ app *App }

func (o skillsOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return replyCardWithIDEffect(ctx, o.app, messageID, card, inThread)
}

func (o skillsOutbound) PatchCard(ctx context.Context, messageID string, card map[string]any) error {
	return patchCardEffect(ctx, o.app, messageID, card)
}

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

func BuildSkillCommands(a *App) *skillsadapter.Service {
	return &skillsadapter.Service{
		Service: a.bindings.Skills, Outbound: skillsOutbound{app: a},
		MakeSessionKey:       func(msg *feishu.InboundMessage) string { return a.configView().makeSessionKey(msg) },
		ReplyInThreadEnabled: func(chatType string) bool { return a.configView().replyInThreadEnabled() },
		FormatMenuBody:       menuCardBody, CommandLabel: commandLabel,
		RunAsync: func(key string, work func()) bool {
			return a.runtimeView().ensureRuntimeOwner().Lifecycle.Run(func() {
				a.sessionActorRuntime().Run("session:"+key, work)
			}, a.asyncRunner)
		},
	}
}
