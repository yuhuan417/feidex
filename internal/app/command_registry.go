package app

import (
	"strconv"
	"strings"

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

func exactCommand(fields []string) bool {
	return len(fields) == 1
}

func exactOrSingleArgCommand(fields []string, allowed ...string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 2 {
		return false
	}
	return commandArgInSet(fields[1], allowed...)
}

func matchBackendCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) == 2 && strings.TrimSpace(fields[1]) == "retry" {
		return true
	}
	if len(fields) != 3 || strings.TrimSpace(fields[1]) != "retry" {
		return false
	}
	return commandArgInSet(fields[2], "status", "on", "off")
}

func commandArgInSet(value string, allowed ...string) bool {
	value = strings.TrimSpace(value)
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func matchReviewCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	switch strings.TrimSpace(fields[1]) {
	case "uncommitted", "uncommittedChanges":
		return len(fields) == 2
	case "base", "commit":
		return len(fields) == 2 || len(fields) == 3
	case "custom":
		return len(fields) >= 2
	default:
		return false
	}
}

func matchHistoryCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 3 || strings.TrimSpace(fields[1]) != "detail" {
		return false
	}
	value, err := strconv.Atoi(strings.TrimSpace(fields[2]))
	return err == nil && value > 0
}

func matchModelCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 3 {
		return false
	}
	switch strings.TrimSpace(fields[1]) {
	case "set", "effort":
		return strings.TrimSpace(fields[2]) != ""
	default:
		return false
	}
}

func matchEffortCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	return len(fields) == 2 && strings.TrimSpace(fields[1]) != ""
}

func matchThreadCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	switch strings.TrimSpace(fields[1]) {
	case "list":
		return len(fields) == 2 || (len(fields) == 3 && strings.TrimSpace(fields[2]) == "all")
	case "new", "fork":
		return len(fields) == 2
	case "resume":
		return len(fields) == 3
	case "sandbox", "policy":
		return len(fields) == 2 || len(fields) == 3
	default:
		return false
	}
}

func matchClaudeThreadCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	switch strings.TrimSpace(fields[1]) {
	case "list":
		return len(fields) == 2 || (len(fields) == 3 && strings.TrimSpace(fields[2]) == "all")
	case "new", "fork":
		return len(fields) == 2
	case "resume":
		return len(fields) == 3
	default:
		return false
	}
}

func matchSessionCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	switch strings.TrimSpace(fields[1]) {
	case "list":
		return len(fields) == 2 || (len(fields) == 3 && strings.TrimSpace(fields[2]) == "all")
	case "new", "fork":
		return len(fields) == 2
	case "resume":
		return len(fields) == 3
	case "permissions":
		return len(fields) == 2 || len(fields) == 3
	default:
		return false
	}
}

func matchUpgradeCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	switch strings.TrimSpace(fields[1]) {
	case "dev", "local":
		return len(fields) == 2
	case "path":
		return len(fields) >= 3
	default:
		if len(fields) != 2 {
			return false
		}
		_, err := normalizeUpgradeVersion(fields[1])
		return err == nil
	}
}

func matchCodexCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 2 {
		return false
	}
	return commandArgInSet(fields[1], "check", "upgrade", "restart")
}

func matchClaudeCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 2 {
		return false
	}
	return commandArgInSet(fields[1], "check", "upgrade", "restart")
}

func matchWorkspaceCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	switch strings.TrimSpace(fields[1]) {
	case "list", "new":
		return len(fields) == 2
	case "delete":
		return len(fields) == 2 || len(fields) == 3
	case "sandbox", "policy":
		return len(fields) == 2 || len(fields) == 3
	case "clone":
		_, _, _, err := parseWorkspaceCloneArgs(fields[1:])
		return err == nil
	case "use":
		return len(fields) == 3
	default:
		return false
	}
}

func matchClaudeWorkspaceCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	switch strings.TrimSpace(fields[1]) {
	case "list", "new":
		return len(fields) == 2
	case "delete":
		return len(fields) == 2 || len(fields) == 3
	case "clone":
		_, _, _, err := parseWorkspaceCloneArgs(fields[1:])
		return err == nil
	case "use":
		return len(fields) == 3
	case "permissions":
		return len(fields) == 2 || len(fields) == 3
	default:
		return false
	}
}

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
	intro := make([]helpCommandSpec, 0, 2)
	sections := map[string][]helpCommandSpec{}
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
		header := backendCapabilityForKind(backend).helpGroupLabel(group)
		lines = append(lines, "", header+"：")
		lines = appendHelpCommands(lines, specs)
	}
	return strings.Join(lines, "\n")
}
