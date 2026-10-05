package feishuapp

import (
	"fmt"
	"strings"

	appfeatures "feidex/internal/application/features"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
)

type localCommandSpec struct {
	appfeatures.CommandSpec
	Handle    CommandHandler
	HandleRaw CommandRawHandler
}

type CommandRegistryInputs struct {
	Features                   FeatureRegistryInputs
	ConfiguredBackend          func() string
	MakeSessionKey             func(*feishu.InboundMessage) string
	WorkspaceConfigured        func() bool
	ReplyBackendSelection      func(*feishu.InboundMessage, string) error
	BackendSwitchBlockedReason func() string
	MaintenanceBlocksCommand   func(string, string) error
	QueuePassthrough           func(string, *feishu.InboundMessage, string) error
}

type CommandRegistry struct {
	specs                      []localCommandSpec
	renderers                  map[string]MenuNodeRenderer
	configuredBackend          func() string
	makeSessionKey             func(*feishu.InboundMessage) string
	workspaceConfigured        func() bool
	replyBackendSelection      func(*feishu.InboundMessage, string) error
	backendSwitchBlockedReason func() string
	maintenanceBlocksCommand   func(string, string) error
	queuePassthrough           func(string, *feishu.InboundMessage, string) error
}

func BuildCommandRegistry(inputs CommandRegistryInputs) CommandRegistry {
	bindings := buildFeatureBindings(inputs.Features)
	return CommandRegistry{
		specs:                      buildLocalCommandSpecs(bindings),
		renderers:                  buildMenuNodeRenderers(bindings),
		configuredBackend:          inputs.ConfiguredBackend,
		makeSessionKey:             inputs.MakeSessionKey,
		workspaceConfigured:        inputs.WorkspaceConfigured,
		replyBackendSelection:      inputs.ReplyBackendSelection,
		backendSwitchBlockedReason: inputs.BackendSwitchBlockedReason,
		maintenanceBlocksCommand:   inputs.MaintenanceBlocksCommand,
		queuePassthrough:           inputs.QueuePassthrough,
	}
}

func findLocalCommandSpecIn(specs []localCommandSpec, name string) *localCommandSpec {
	for i := range specs {
		spec := &specs[i]
		for _, candidate := range spec.Names {
			if candidate == name {
				return spec
			}
		}
	}
	return nil
}

func (r *CommandRegistry) Handle(msg *feishu.InboundMessage, raw string) error {
	raw = strings.TrimSpace(raw)
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	spec := findLocalCommandSpecIn(r.specs, fields[0])
	if spec == nil {
		return fmt.Errorf("unknown command: %s", fields[0])
	}
	backend := ""
	if r.configuredBackend != nil {
		backend = r.configuredBackend()
	}
	if strings.TrimSpace(backend) == "" && !commandAllowedWithoutBackend(msg, fields[0]) {
		if r.replyBackendSelection == nil {
			return nil
		}
		return r.replyBackendSelection(msg, "")
	}
	if r.backendSwitchBlockedReason != nil {
		if reason := r.backendSwitchBlockedReason(); reason != "" {
			return conversation.NewWarning(reason)
		}
	}
	if r.maintenanceBlocksCommand != nil {
		if err := r.maintenanceBlocksCommand(backend, raw); err != nil {
			return err
		}
	}
	if !commandHandlesLocallyForBackend(spec, backend, fields) {
		if r.queuePassthrough == nil {
			return nil
		}
		sessionKey := ""
		if r.makeSessionKey != nil {
			sessionKey = r.makeSessionKey(msg)
		}
		return r.queuePassthrough(sessionKey, msg, raw)
	}
	if spec.HandleRaw != nil {
		return spec.HandleRaw(msg, raw, fields[1:])
	}
	if spec.Handle == nil {
		return nil
	}
	return spec.Handle(msg, fields[1:])
}

func (r *CommandRegistry) RenderFallback(actionName, sessionKey string) (map[string]any, bool) {
	if r == nil || r.workspaceConfigured == nil || !r.workspaceConfigured() {
		return nil, false
	}
	backend := ""
	if r.configuredBackend != nil {
		backend = r.configuredBackend()
	}
	actionName = nearestVisibleMenuAction(actionName, backend)
	renderer := r.renderers[actionName]
	if renderer == nil {
		return nil, false
	}
	return renderer(sessionKey)
}

func commandHandlesLocallyForBackend(spec *localCommandSpec, backend string, fields []string) bool {
	if spec == nil {
		return false
	}
	return appfeatures.HandlesCommand(backend, strings.Join(fields, " "))
}

func renderHelpBodyFromRegistry(backend string) string {
	return renderHelpBodyFromRegistryScoped(backend, false)
}

func renderHelpBodyForSession(scope bindingSessionScope, backend, sessionKey string) string {
	return renderHelpBodyFromRegistryScoped(backend, groupBindingSessionScopeActive(scope, sessionKey))
}

func renderHelpBodyFromRegistryScoped(backend string, groupScoped bool) string {
	return appfeatures.HelpBody(backend, groupScoped)
}
