package feishuapp

import (
	"strings"
	"sync"

	appfeatures "feidex/internal/application/features"
	"feidex/internal/feishu"
)

type featureCommandBinding struct {
	Handle    func(a *App, msg *feishu.InboundMessage, args []string) error
	HandleRaw func(a *App, msg *feishu.InboundMessage, raw string, args []string) error
}

type featureBinding struct {
	Commands      map[string]featureCommandBinding
	RenderActions []string
	Render        func(actionName string, a *App, sessionKey string) (map[string]any, bool)
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
	featureBindingsOnce     sync.Once
	cachedFeatureBindings   map[string]featureBinding
	localCommandSpecsOnce   sync.Once
	cachedLocalCommandSpecs []localCommandSpec
	menuNodeRenderersOnce   sync.Once
	cachedMenuNodeRenderers map[string]menuNodeRenderer
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

type menuNodeRenderer func(a *App, sessionKey string) (map[string]any, bool)
