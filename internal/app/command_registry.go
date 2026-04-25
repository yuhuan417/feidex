package app

import (
	"strings"

	appcommandmatch "feidex/internal/app/commandmatch"
	"feidex/internal/app/menutypes"
	"feidex/internal/feishu"
)

type localCommandSpec struct {
	Names       []string
	IsLocal     func(fields []string) bool
	Handle      func(a *App, msg *feishu.InboundMessage, args []string) error
	HelpGroup   string
	HelpEntries []helpCommandSpec
	Backends    map[string]localCommandBackendSpec
}

type localCommandBackendSpec struct {
	Match       func(fields []string) bool
	HideInHelp  bool
	HelpEntries []helpCommandSpec
}

var exactCommand = appcommandmatch.ExactCommand

var exactOrSingleArgCommand = appcommandmatch.ExactOrSingleArgCommand

var matchBackendCommand = appcommandmatch.MatchBackendCommand

var commandArgInSet = appcommandmatch.CommandArgInSet

var matchReviewCommand = appcommandmatch.MatchReviewCommand

var matchHistoryCommand = appcommandmatch.MatchHistoryCommand

var matchModelCommand = appcommandmatch.MatchModelCommand

var matchEffortCommand = appcommandmatch.MatchEffortCommand

var matchThreadCommand = appcommandmatch.MatchThreadCommand

var matchSessionCommand = appcommandmatch.MatchSessionCommand

var matchUpgradeCommand = appcommandmatch.MatchUpgradeCommand

var matchCodexCommand = appcommandmatch.MatchCodexCommand

var matchClaudeCommand = appcommandmatch.MatchClaudeCommand

var matchWorkspaceCommand = appcommandmatch.MatchWorkspaceCommand

var matchClaudeWorkspaceCommand = appcommandmatch.MatchClaudeWorkspaceCommand

var normalizeUpgradeVersion = appcommandmatch.NormalizeUpgradeVersion

func hiddenBackendCommand() localCommandBackendSpec {
	return localCommandBackendSpec{HideInHelp: true}
}

func partialBackendCommand(match func([]string) bool, helpEntries []helpCommandSpec) localCommandBackendSpec {
	return localCommandBackendSpec{
		Match:       match,
		HelpEntries: append([]helpCommandSpec(nil), helpEntries...),
	}
}

func localCommandSpecList() []localCommandSpec {
	specs := make([]localCommandSpec, 0, 25)
	specs = append(specs, localCommandSystemIntroSpecs()...)
	specs = append(specs, localCommandCommonToolSpecs()...)
	specs = append(specs, localCommandModelSpecs()...)
	specs = append(specs, localCommandSystemDebugSpecs()...)
	specs = append(specs, localCommandFastSpecs()...)
	specs = append(specs, localCommandWorkspaceToolSpecs()...)
	specs = append(specs, localCommandConversationSpecs()...)
	specs = append(specs, localCommandSystemRuntimeSpecs()...)
	specs = append(specs, localCommandWorkspaceSpecs()...)
	return specs
}

var helpGroupOrder = []string{
	"常用工具",
	"model",
	"thread",
	"workspace",
	"system",
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

func (s localCommandSpec) backendPolicy(backend string) localCommandBackendSpec {
	backend = normalizeRuntimeBackend(backend)
	if policy, ok := s.Backends[backend]; ok {
		return policy
	}
	return localCommandBackendSpec{
		Match: s.IsLocal,
	}
}

func (s localCommandSpec) helpEntriesForBackend(backend string) []helpCommandSpec {
	backend = normalizeRuntimeBackend(backend)
	if policy, ok := s.Backends[backend]; ok {
		if policy.HideInHelp {
			return nil
		}
		if policy.HelpEntries != nil {
			return append([]helpCommandSpec(nil), policy.HelpEntries...)
		}
	}
	return append([]helpCommandSpec(nil), s.HelpEntries...)
}

func commandHandlesLocallyForBackend(spec *localCommandSpec, backend string, fields []string) bool {
	if spec == nil {
		return false
	}
	policy := spec.backendPolicy(backend)
	if policy.Match == nil {
		return false
	}
	return policy.Match(fields)
}

func renderHelpBodyFromRegistry(backend string) string {
	lines := []string{"命令说明：", ""}
	intro := make([]menutypes.HelpCommandSpec, 0, 2)
	sections := map[string][]menutypes.HelpCommandSpec{}
	for _, spec := range localCommandSpecList() {
		entries := spec.helpEntriesForBackend(backend)
		if len(entries) == 0 {
			continue
		}
		if strings.TrimSpace(spec.HelpGroup) == "" {
			intro = append(intro, entries...)
			continue
		}
		group := strings.TrimSpace(spec.HelpGroup)
		sections[group] = append(sections[group], entries...)
	}
	lines = appendHelpCommands(lines, intro)
	for _, group := range helpGroupOrder {
		specs := sections[group]
		if len(specs) == 0 {
			continue
		}
		header := backendCapabilityForKind(backend).HelpGroupLabel(group)
		lines = append(lines, "", header+"：")
		lines = appendHelpCommands(lines, specs)
	}
	return strings.Join(lines, "\n")
}
