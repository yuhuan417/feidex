package feishuapp

import (
	"context"
	"sync"

	appbackend "feidex/internal/adapter/feishu/backend"
	modelcommands "feidex/internal/adapter/feishu/modelconfig"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	tier "feidex/internal/adapter/feishu/servicetier"
	"feidex/internal/adapter/feishu/workspace"
	"feidex/internal/adapter/feishu/workspacecmd"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application"
	"feidex/internal/application/frontend"
	"feidex/internal/application/interaction"
	"feidex/internal/application/modelconfig"
	"feidex/internal/application/routing"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/compositionkit"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	"feidex/internal/runtime"
)

type BindingCommandInputs struct {
	Scope                  BindingScope
	Config                 *config.Config
	ConfigMu               *sync.RWMutex
	State                  *appstate.Store
	FrontendID             string
	ConfiguredBackend      func() string
	MakeSessionKey         func(*feishu.InboundMessage) string
	Context                func() context.Context
	Effects                runtime.EffectRunner
	RunAsync               func(func())
	RefreshGroupStatus     func(string)
	CurrentBotDisplayName  func() string
	CurrentLiveBotOpenID   func() string
	InitializeGroupPrimary func(context.Context, string, string) error
	SimpleStatusCard       func(string, string, string, []feishu.Button) map[string]any

	BackendConfiguration       appbackend.ConfigurationService
	Forms                      *interaction.FormService
	FrontendQuery              frontend.Query
	GroupWorkspaces            workspaceapp.GroupService
	ModelCommands              modelcommands.ModelConfigService
	ModelSnapshots             modelconfig.SnapshotService
	Primary                    routing.Service
	RoutingConfiguration       compositionkit.RoutingConfiguration
	ScopedRoutingConfiguration compositionkit.ScopedRoutingConfiguration
	ServiceTier                tier.Service
	WorkspaceConfiguration     *workspacecmd.ConfigService
	WorkspaceManagement        *workspacecmd.ManagementService
	WorkspacePresentation      *workspace.Presentation
	WorkspaceWorkflow          *workspaceapp.Workflow
}

func (d BindingCommandInputs) replyCard(msg *feishu.InboundMessage, card map[string]any) error {
	if msg == nil {
		return nil
	}
	return d.Effects.Run(context.Background(), []application.Effect{application.SendCard{
		Frontend:       identity.FrontendID(d.FrontendID),
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		View:           feishuoutbound.Card(card),
	}})
}

func (d BindingCommandInputs) replyCardWithID(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return d.Effects.RunSendCard(ctx, application.SendCard{
		Frontend: identity.FrontendID(d.FrontendID), ReplyMessageID: messageID,
		View: feishuoutbound.Card(card), InThread: inThread,
	})
}

func (d BindingCommandInputs) patchCard(ctx context.Context, messageID string, card map[string]any) error {
	return d.Effects.Run(ctx, []application.Effect{application.PatchCard{
		Frontend: identity.FrontendID(d.FrontendID), MessageID: messageID,
		View: feishuoutbound.Card(card), IdempotencyKey: cardEffectKey("patch-card", d.FrontendID, messageID, card),
	}})
}
