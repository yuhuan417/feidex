package feishuapp

import (
	"strings"

	appfeatures "feidex/internal/application/features"
	"feidex/internal/feishu"
)

type localCommandSpec struct {
	appfeatures.CommandSpec
	Handle    func(a *App, msg *feishu.InboundMessage, args []string) error
	HandleRaw func(a *App, msg *feishu.InboundMessage, raw string, args []string) error
}

func localCommandSpecList() []localCommandSpec {
	return localCommandSpecsRegistry()
}

func findLocalCommandSpec(name string) *localCommandSpec {
	specs := localCommandSpecList()
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

func commandHandlesLocallyForBackend(spec *localCommandSpec, backend string, fields []string) bool {
	if spec == nil {
		return false
	}
	return appfeatures.HandlesCommand(backend, strings.Join(fields, " "))
}

func renderHelpBodyFromRegistry(backend string) string {
	return renderHelpBodyFromRegistryScoped(backend, false)
}

func renderHelpBodyForSession(a *App, backend, sessionKey string) string {
	return renderHelpBodyFromRegistryScoped(backend, groupBindingSessionScopeActive(a.bindings.BindingCommands.scope, sessionKey))
}

func renderHelpBodyFromRegistryScoped(backend string, groupScoped bool) string {
	return appfeatures.HelpBody(backend, groupScoped)
}
