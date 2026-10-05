package feishuapp

import (
	"strings"
	"sync"

	appfeatures "feidex/internal/application/features"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type featureCommandBinding struct {
	Handle    func(a *App, msg *feishu.InboundMessage, args []string) error
	HandleRaw func(a *App, msg *feishu.InboundMessage, raw string, args []string) error
}

type featureBinding struct {
	Commands      map[string]featureCommandBinding
	RenderActions []string
	Render        func(actionName string, a *App, sessionKey string) (map[string]any, bool)
	HandleAction  func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error)
	PortActions   []string
}

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

func menuNodeRenderers() map[string]menuNodeRenderer {
	menuNodeRenderersOnce.Do(func() {
		cachedMenuNodeRenderers = buildMenuNodeRenderers()
	})
	return cachedMenuNodeRenderers
}

func menuCardActionHandlers() map[string]cardActionHandler {
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
				CommandSpec: command,
				Handle:      commandBinding.Handle,
				HandleRaw:   commandBinding.HandleRaw,
			}
			specs = append(specs, spec)
		}
	}
	return specs
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
		if !ok {
			panic("missing feature action binding for " + feature.ID)
		}
		for _, actionName := range feature.ActionNames {
			name := strings.TrimSpace(actionName.String())
			if name == "" {
				continue
			}
			if containsActionName(binding.PortActions, name) {
				continue
			}
			if binding.HandleAction == nil {
				panic("missing feature action handler for " + feature.ID + ": " + name)
			}
			handlers[name] = func(actionName string, binding featureBinding) cardActionHandler {
				return func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
					return binding.HandleAction(actionName, s, action)
				}
			}(name, binding)
		}
	}
	delete(handlers, "history.page")
	delete(handlers, "history.detail")
	delete(handlers, "history.detail.select")
	return handlers
}

func containsActionName(names []string, target string) bool {
	for _, name := range names {
		if name == target {
			return true
		}
	}
	return false
}

type menuNodeRenderer func(a *App, sessionKey string) (map[string]any, bool)
