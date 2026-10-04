package feishuapp

import (
	"sync"

	appbackend "feidex/internal/adapter/feishu/backend"
	modelcommands "feidex/internal/adapter/feishu/modelconfig"
	"feidex/internal/application/workspace"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type BackendConfigurationInputs struct {
	Config              *config.Config
	ConfigMu            *sync.RWMutex
	Backend             func() string
	FrontendConfigIndex int
	Store               *state.Store
	WorkspaceSelection  workspace.SelectionService
	Driver              appbackend.Driver
	ModelCommands       modelcommands.ModelConfigService
}

type backendConfigurationPermissions struct {
	config              *config.Config
	configMu            *sync.RWMutex
	backend             func() string
	frontendConfigIndex int
	store               *state.Store
	workspaceSelection  workspace.SelectionService
}

func (p backendConfigurationPermissions) Config() *config.Config  { return p.config }
func (p backendConfigurationPermissions) ConfigMu() *sync.RWMutex { return p.configMu }
func (p backendConfigurationPermissions) Backend() string {
	if p.backend == nil {
		return ""
	}
	return p.backend()
}
func (p backendConfigurationPermissions) FrontendConfigIndex() int { return p.frontendConfigIndex }
func (p backendConfigurationPermissions) Store() *state.Store      { return p.store }
func (p backendConfigurationPermissions) WorkspaceSelection() workspace.SelectionService {
	return p.workspaceSelection
}

func buildBackendConfigurationService(inputs BackendConfigurationInputs) appbackend.ConfigurationService {
	modelCommands := inputs.ModelCommands

	inner := appbackend.NewConfigurationService(appbackend.ConfigurationDeps{
		Permissions: backendConfigurationPermissions{
			config: inputs.Config, configMu: inputs.ConfigMu, backend: inputs.Backend,
			frontendConfigIndex: inputs.FrontendConfigIndex, store: inputs.Store,
			workspaceSelection: inputs.WorkspaceSelection,
		},
		Driver: inputs.Driver,
		Formatting: appbackend.ConfigurationFormattingDeps{
			FormatMenuBody: menuCardBody,
		},
		Commands: appbackend.ConfigurationCommandDeps{
			HandleCodexModelCommand: func(msg *feishu.InboundMessage, args []string) error {
				return modelCommands.CommandCodexModel(msg, args)
			},
			HandleClaudeModelCommand: func(msg *feishu.InboundMessage, args []string) error {
				return modelCommands.CommandClaudeModel(msg, args)
			},
		},
		Claude: appbackend.ConfigurationClaudeDeps{
			CompleteModelSet: func(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
				return modelCommands.CompleteClaudeModelSet(action, modelID)
			},
			CompleteModelOptionAdd: func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				return modelCommands.CompleteClaudeModelOptionAdd(action)
			},
			CompleteModelOptionRemove: func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				return modelCommands.CompleteClaudeModelOptionRemove(action)
			},
			CompleteEffortSet: func(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
				return modelCommands.CompleteClaudeEffortSet(action, effort)
			},
		},
		Codex: appbackend.ConfigurationCodexDeps{
			CompleteCodexGlobalModelSet: func(action *feishu.CardAction, value string) (*callback.CardActionTriggerResponse, error) {
				return modelCommands.CompleteCodexGlobalModelSet(action, value)
			},
			CompleteCodexGlobalReasoningEffortSet: func(action *feishu.CardAction, value string) (*callback.CardActionTriggerResponse, error) {
				return modelCommands.CompleteCodexGlobalReasoningEffortSet(action, value)
			},

			FetchModelList:                   modelCommands.FetchModelList,
			FetchPlanCollaborationModePreset: modelCommands.FetchPlanCollaborationModePreset,
			RenderModelConfigCard:            modelCommands.RenderModelConfigCard,
		},
	})
	return inner
}
