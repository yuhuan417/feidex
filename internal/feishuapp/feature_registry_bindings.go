package feishuapp

import (
	"strings"

	appfeatures "feidex/internal/application/features"
	"feidex/internal/feishu"
)

type CommandHandler func(*feishu.InboundMessage, []string) error
type CommandRawHandler func(*feishu.InboundMessage, string, []string) error
type MenuNodeRenderer func(string) (map[string]any, bool)

type FeatureRegistryInputs struct {
	Commands    map[string]CommandHandler
	RawCommands map[string]CommandRawHandler
	Renderers   map[string]MenuNodeRenderer
}

type CommandFeatureInputs struct {
	Menu, Backend, Primary, Review, Quiet, Plan, Compact, Download CommandHandler
	History, Skills, Usage, Interrupt, Fork, New, Thread, Session  CommandHandler
	Threads, Workspace, Model, Effort, Fast, Debug, Status, Help   CommandHandler
	Codex, Claude, Upgrade                                         CommandHandler
	Goal                                                           CommandRawHandler

	MenuRoot, MenuTools, MenuGroupModel, MenuGroupSystem   MenuNodeRenderer
	MenuGroupBackend, MenuCurrentBot, MenuCurrentWorkspace MenuNodeRenderer
	MenuThread, MenuWorkspace, MenuReview, MenuSkills      MenuNodeRenderer
	MenuDebugLogs, MenuCodexUpgrade, MenuClaudeUpgrade     MenuNodeRenderer
}

func BuildFeatureRegistryInputs(inputs CommandFeatureInputs) FeatureRegistryInputs {
	return FeatureRegistryInputs{
		Commands: map[string]CommandHandler{
			"menu": inputs.Menu, "backend": inputs.Backend, "primary": inputs.Primary,
			"review": inputs.Review, "quiet": inputs.Quiet, "plan": inputs.Plan,
			"compact": inputs.Compact, "download": inputs.Download, "history": inputs.History,
			"skills": inputs.Skills, "usage": inputs.Usage, "interrupt": inputs.Interrupt,
			"fork": inputs.Fork, "new": inputs.New, "thread": inputs.Thread,
			"session": inputs.Session, "threads": inputs.Threads, "workspace": inputs.Workspace,
			"model": inputs.Model, "effort": inputs.Effort, "fast": inputs.Fast,
			"debug": inputs.Debug, "status": inputs.Status, "help": inputs.Help,
			"codex": inputs.Codex, "claude": inputs.Claude, "upgrade": inputs.Upgrade,
		},
		RawCommands: map[string]CommandRawHandler{"goal": inputs.Goal},
		Renderers: map[string]MenuNodeRenderer{
			"menu.root": inputs.MenuRoot, "menu.tools": inputs.MenuTools,
			"menu.group.model": inputs.MenuGroupModel, "menu.group.system": inputs.MenuGroupSystem,
			"menu.group.backend": inputs.MenuGroupBackend, "menu.current_bot": inputs.MenuCurrentBot,
			"menu.current_workspace": inputs.MenuCurrentWorkspace, "menu.thread": inputs.MenuThread,
			"menu.workspace": inputs.MenuWorkspace, "menu.review": inputs.MenuReview,
			"menu.skills": inputs.MenuSkills, "menu.debug.logs": inputs.MenuDebugLogs,
			"menu.codex_upgrade": inputs.MenuCodexUpgrade, "menu.claude_upgrade": inputs.MenuClaudeUpgrade,
		},
	}
}

type featureCommandBinding struct {
	Handle    CommandHandler
	HandleRaw CommandRawHandler
}

type featureBinding struct {
	Commands      map[string]featureCommandBinding
	RenderActions []string
	Render        func(actionName string, sessionKey string) (map[string]any, bool)
	PortActions   []string
}

func (i FeatureRegistryInputs) command(name string) featureCommandBinding {
	return featureCommandBinding{Handle: i.Commands[name], HandleRaw: i.RawCommands[name]}
}

func (i FeatureRegistryInputs) render(actionName string) func(string, string) (map[string]any, bool) {
	return func(actionName, sessionKey string) (map[string]any, bool) {
		render := i.Renderers[actionName]
		if render == nil {
			return nil, false
		}
		return render(sessionKey)
	}
}

func buildFeatureBindings(inputs FeatureRegistryInputs) map[string]featureBinding {
	bindings := map[string]featureBinding{}
	appendFeatureBindingsMenuCore(bindings, inputs)
	appendFeatureBindingsBinding(bindings, inputs)
	appendFeatureBindingsTools(bindings, inputs)
	appendFeatureBindingsThreadWorkspace(bindings, inputs)
	appendFeatureBindingsSystem(bindings, inputs)
	return bindings
}

func buildLocalCommandSpecs(bindings map[string]featureBinding) []localCommandSpec {
	specs := make([]localCommandSpec, 0, 32)
	for _, feature := range appfeatures.All() {
		binding, ok := bindings[feature.ID]
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

func buildMenuNodeRenderers(bindings map[string]featureBinding) map[string]MenuNodeRenderer {
	renderers := map[string]MenuNodeRenderer{}
	for _, feature := range appfeatures.All() {
		binding, ok := bindings[feature.ID]
		if !ok || binding.Render == nil {
			continue
		}
		for _, actionName := range binding.RenderActions {
			name := strings.TrimSpace(actionName)
			if name == "" {
				continue
			}
			renderers[name] = func(actionName string, binding featureBinding) MenuNodeRenderer {
				return func(sessionKey string) (map[string]any, bool) {
					return binding.Render(actionName, sessionKey)
				}
			}(name, binding)
		}
	}
	return renderers
}
