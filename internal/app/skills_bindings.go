package app

import (
	"context"
	skillsadapter "feidex/internal/adapter/feishu/skills"
	skillapp "feidex/internal/application/skill"
	"feidex/internal/composition"
	"feidex/internal/domain/identity"
	skillcatalog "feidex/internal/domain/skill"
	"feidex/internal/feishu"
	skillruntime "feidex/internal/runtime/skill"
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
type skillsCatalog struct{ app *App }

func (c skillsCatalog) ListSkills(ctx context.Context, cwd string, reload bool) (skillcatalog.SkillsListEntry, error) {
	gateway, err := requireCodexGateway(c.app)
	if err != nil {
		return skillcatalog.SkillsListEntry{}, err
	}
	return gateway.ListSkills(ctx, cwd, reload)
}

func newSkillUseCase(a *App) *skillapp.Service {
	trackers := a.Trackers()
	a.composition.mu.Lock()
	if trackers.pendingSkills == nil {
		trackers.pendingSkills = skillruntime.NewTracker()
	}
	pending := trackers.pendingSkills
	a.composition.mu.Unlock()
	return composition.NewSkillService(composition.SkillDependencies{
		Frontend: identity.FrontendID(a.FrontendID()),
		Context:  a.Context, Config: a.cfg, Mutex: a.ConfigMu(),
		Sessions: a.State(), Pending: pending,
		Catalog: skillsCatalog{app: a},
	})
}

func newSkillsService(a *App) *skillsadapter.Service {
	return &skillsadapter.Service{
		Service: newSkillUseCase(a), Outbound: skillsOutbound{app: a},
		MakeSessionKey:       func(msg *feishu.InboundMessage) string { return makeSessionKey(a, msg) },
		ReplyInThreadEnabled: func(chatType string) bool { return replyInThreadEnabled(a, chatType) },
		FormatMenuBody:       menuCardBody, CommandLabel: commandLabel,
		RunAsync: func(key string, work func()) bool {
			return a.frontendRuntime.Run(func() {
				a.sessionActorRuntime().Run("session:"+key, work)
			}, a.asyncRunner)
		},
	}
}
