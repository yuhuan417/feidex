package app

import (
	"strings"
	"sync"

	appfeatures "feidex/internal/app/features"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type featureCommandBinding struct {
	Match     func(fields []string) bool
	Handle    func(a *App, msg *feishu.InboundMessage, args []string) error
	HandleRaw func(a *App, msg *feishu.InboundMessage, raw string, args []string) error
	Backends  map[string]func(fields []string) bool
}

type featureBinding struct {
	Commands      map[string]featureCommandBinding
	RenderActions []string
	Render        func(actionName string, a *App, sessionKey string) (map[string]any, bool)
	HandleAction  func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error)
}

var registryBackends = []string{backendCodex, backendClaude}

func buildFeatureBindings() map[string]featureBinding {
	bindings := map[string]featureBinding{}
	appendFeatureBindingsMenuCore(bindings)
	appendFeatureBindingsBinding(bindings)
	appendFeatureBindingsTools(bindings)
	appendFeatureBindingsThreadWorkspace(bindings)
	appendFeatureBindingsSystem(bindings)
	return bindings
}

func featureBindingForID(id string) (featureBinding, bool) {
	binding, ok := featureBindingsRegistry()[id]
	return binding, ok
}

var (
	featureBindingsOnce          sync.Once
	cachedFeatureBindings        map[string]featureBinding
	localCommandSpecsOnce        sync.Once
	cachedLocalCommandSpecs      []localCommandSpec
	menuNodeRenderersOnce        sync.Once
	cachedMenuNodeRenderers      map[string]menuNodeRenderer
	menuCardActionHandlersOnce   sync.Once
	cachedMenuCardActionHandlers map[string]cardActionHandler
)

func featureBindingsRegistry() map[string]featureBinding {
	featureBindingsOnce.Do(func() {
		cachedFeatureBindings = buildFeatureBindings()
	})
	return cachedFeatureBindings
}

func localCommandSpecsRegistry() []localCommandSpec {
	localCommandSpecsOnce.Do(func() {
		cachedLocalCommandSpecs = buildLocalCommandSpecs()
	})
	return append([]localCommandSpec(nil), cachedLocalCommandSpecs...)
}

func menuNodeRenderersRegistry() map[string]menuNodeRenderer {
	menuNodeRenderersOnce.Do(func() {
		cachedMenuNodeRenderers = buildMenuNodeRenderers()
	})
	return cachedMenuNodeRenderers
}

func menuCardActionHandlersRegistry() map[string]cardActionHandler {
	menuCardActionHandlersOnce.Do(func() {
		cachedMenuCardActionHandlers = buildMenuCardActionHandlers()
	})
	return cachedMenuCardActionHandlers
}

func buildLocalCommandSpecs() []localCommandSpec {
	specs := make([]localCommandSpec, 0, 32)
	for _, feature := range appfeatures.All() {
		binding, ok := featureBindingForID(feature.ID)
		if !ok {
			if len(feature.Commands) > 0 {
				panic("missing feature command binding for " + feature.ID)
			}
			continue
		}
		for _, command := range feature.Commands {
			commandBinding, ok := binding.Commands[command.ID]
			if !ok {
				panic("missing command binding for feature " + feature.ID + " command " + command.ID)
			}
			spec := localCommandSpec{
				Names:       append([]string(nil), command.Names...),
				IsLocal:     commandBinding.Match,
				Handle:      commandBinding.Handle,
				HandleRaw:   commandBinding.HandleRaw,
				HelpGroup:   strings.TrimSpace(command.HelpGroup),
				HelpEntries: append([]helpCommandSpec(nil), command.HelpEntries...),
				Backends:    buildLocalCommandBackendPolicies(feature, command, commandBinding),
			}
			specs = append(specs, spec)
		}
	}
	return specs
}

func buildLocalCommandBackendPolicies(feature appfeatures.Spec, command appfeatures.CommandSpec, binding featureCommandBinding) map[string]localCommandBackendSpec {
	policies := map[string]localCommandBackendSpec{}
	for _, backend := range registryBackends {
		metaPolicy, hasMetaPolicy := command.Backends[backend]
		match := binding.Match
		hasBindingPolicy := false
		if binding.Backends != nil {
			if backendMatch, ok := binding.Backends[backend]; ok {
				match = backendMatch
				hasBindingPolicy = true
			}
		}
		if !feature.SupportsBackend(backend) {
			match = nil
			metaPolicy.HideInHelp = true
			hasMetaPolicy = true
			hasBindingPolicy = true
		}
		if !hasMetaPolicy && !hasBindingPolicy {
			continue
		}
		policies[backend] = localCommandBackendSpec{
			Match:       match,
			HideInHelp:  metaPolicy.HideInHelp,
			HelpEntries: append([]helpCommandSpec(nil), metaPolicy.HelpEntries...),
		}
	}
	if len(policies) == 0 {
		return nil
	}
	return policies
}

func buildMenuNodeRenderers() map[string]menuNodeRenderer {
	renderers := map[string]menuNodeRenderer{}
	for _, feature := range appfeatures.All() {
		binding, ok := featureBindingForID(feature.ID)
		if !ok || binding.Render == nil {
			continue
		}
		for _, actionName := range binding.RenderActions {
			name := strings.TrimSpace(actionName)
			if name == "" {
				continue
			}
			renderers[name] = func(actionName string, binding featureBinding) menuNodeRenderer {
				return func(a *App, sessionKey string) (map[string]any, bool) {
					return binding.Render(actionName, a, sessionKey)
				}
			}(name, binding)
		}
	}
	return renderers
}

func buildMenuCardActionHandlers() map[string]cardActionHandler {
	handlers := map[string]cardActionHandler{}
	for _, feature := range appfeatures.All() {
		if len(feature.ActionNames) == 0 {
			continue
		}
		binding, ok := featureBindingForID(feature.ID)
		if !ok || binding.HandleAction == nil {
			panic("missing feature action binding for " + feature.ID)
		}
		for _, actionName := range feature.ActionNames {
			name := strings.TrimSpace(actionName.String())
			if name == "" {
				continue
			}
			handlers[name] = func(actionName string, binding featureBinding) cardActionHandler {
				return func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
					return binding.HandleAction(actionName, s, action)
				}
			}(name, binding)
		}
	}
	return handlers
}
