package app

import (
	"fmt"
	"strings"

	appbackend "feidex/internal/app/backend"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// backendConfigurationService wraps backend.ConfigurationService to preserve
// the lowercase method names used throughout app/.
type backendConfigurationService struct {
	app   *App
	inner appbackend.ConfigurationService
}

func newBackendConfigurationService(app *App) backendConfigurationService {
	inner := appbackend.NewConfigurationService(appbackend.ConfigurationDeps{
		App: app,
		Formatting: appbackend.ConfigurationFormattingDeps{
			FormatMenuBody: menuCardBody,
		},
		Commands: appbackend.ConfigurationCommandDeps{
			HandleModelCommand: func(msg *feishu.InboundMessage, args []string) error {
				switch configuredBackend(app) {
				case backendClaude:
					return newModelConfigService(app).commandClaudeModel(msg, args)
				default:
					return newModelConfigService(app).commandCodexModel(msg, args)
				}
			},
			HandleWorkspacePermissionCommand: func(msg *feishu.InboundMessage, args []string, sessionKey string) error {
				switch configuredBackend(app) {
				case backendClaude:
					if len(args) == 1 {
						return showClaudeWorkspacePermissionMenu(app, msg)
					}
					if len(args) != 2 {
						return fmt.Errorf("usage: /workspace permissions [MODE|inherit]")
					}
					_, _, ws := newWorkspaceConfigService(app).currentWorkspaceForMessage(msg)
					if ws == nil {
						return fmt.Errorf("workspace not found")
					}
					resp, err := newWorkspaceService(app).completeClaudeWorkspacePermissionModeSet(commandActionFromMessage(msg, nil), sessionKey, ws.ID, strings.TrimSpace(args[1]))
					if err != nil {
						return err
					}
					return replyCommandActionResponse(app, msg, resp)
				default:
					return fmt.Errorf("usage: %s", workspaceCommandUsage)
				}
			},
		},
		Claude: appbackend.ConfigurationClaudeDeps{
			CompleteModelSet: func(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
				return newModelConfigService(app).completeClaudeModelSet(action, modelID)
			},
			CompleteEffortSet: func(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
				return newModelConfigService(app).completeClaudeEffortSet(action, effort)
			},
		},
		Codex: appbackend.ConfigurationCodexDeps{
			FetchModelList:          newModelConfigService(app).fetchModelList,
			UpdateGlobalModelConfig: newModelConfigService(app).updateGlobalModelConfig,
			RenderModelConfigCard:   newModelConfigService(app).renderModelConfigCard,
		},
	})
	return backendConfigurationService{app: app, inner: inner}
}

func (s backendConfigurationService) backendWorkspaceCommandUsage() string {
	return s.inner.BackendWorkspaceCommandUsage()
}

func (s backendConfigurationService) handleBackendModelCommand(msg *feishu.InboundMessage, args []string) error {
	return s.inner.HandleBackendModelCommand(msg, args)
}

func (s backendConfigurationService) handleBackendWorkspacePermissionCommand(msg *feishu.InboundMessage, args []string, sessionKey string) error {
	return s.inner.HandleBackendWorkspacePermissionCommand(msg, args, sessionKey)
}

func (s backendConfigurationService) appendBackendWorkspaceSummaryLines(lines []string, currentWS *config.Workspace) []string {
	return s.inner.AppendBackendWorkspaceSummaryLines(lines, currentWS)
}

func (s backendConfigurationService) backendWorkspaceConfigButtons(sessionKey string) []feishu.Button {
	return s.inner.BackendWorkspaceConfigButtons(sessionKey)
}

func (s backendConfigurationService) backendWorkspaceSwitchInFlightNotice() string {
	return s.inner.BackendWorkspaceSwitchInFlightNotice()
}

func (s backendConfigurationService) backendWorkspaceSwitchBindingFailureNotice() string {
	return s.inner.BackendWorkspaceSwitchBindingFailureNotice()
}

func (s backendConfigurationService) backendWorkspaceSwitchBindingNotice(binding *workspaceThreadBinding) string {
	return s.inner.BackendWorkspaceSwitchBindingNotice(binding)
}

func (s backendConfigurationService) renderModelMenuCard(sessionKey string) map[string]any {
	return s.inner.RenderModelMenuCard(sessionKey)
}

func (s backendConfigurationService) completeGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteGlobalModelSet(action, modelID)
}

func (s backendConfigurationService) completeGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteGlobalReasoningEffortSet(action, reasoningEffort)
}

func (s backendConfigurationService) statusCardBody(sess *state.Session) string {
	return s.inner.StatusCardBody(sess)
}
