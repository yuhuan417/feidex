package app

import (
	"strings"

	appcommandmatch "feidex/internal/app/commandmatch"
	appfeatures "feidex/internal/app/features"
	"feidex/internal/app/menutypes"
	"feidex/internal/feishu"
)

type localCommandSpec struct {
	Names       []string
	IsLocal     func(fields []string) bool
	Handle      func(a *App, msg *feishu.InboundMessage, args []string) error
	HandleRaw   func(a *App, msg *feishu.InboundMessage, raw string, args []string) error
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

var matchGoalCommand = appcommandmatch.MatchGoalCommand

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

func localCommandSpecList() []localCommandSpec {
	return localCommandSpecsRegistry()
}

var helpGroupOrder = appfeatures.HelpGroupOrder()

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
	return renderHelpBodyFromRegistryScoped(backend, false)
}

func renderHelpBodyForSession(backend, sessionKey string) string {
	return renderHelpBodyFromRegistryScoped(backend, isGroupSessionKey(sessionKey))
}

func renderHelpBodyFromRegistryScoped(backend string, groupScoped bool) string {
	lines := []string{"命令说明：", ""}
	intro := make([]menutypes.HelpCommandSpec, 0, 2)
	sections := map[string][]menutypes.HelpCommandSpec{}
	for _, spec := range localCommandSpecList() {
		entries := spec.helpEntriesForBackend(backend)
		if groupScoped {
			entries = groupScopedHelpEntries(entries)
		}
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
	if groupScoped {
		sections["workspace"] = append(sections["workspace"], menutypes.HelpCommandSpec{Command: "/primary on|off", Summary: "设置当前 Bot 是否处理本群未明确 @ 的消息。"})
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

func groupScopedHelpEntries(entries []helpCommandSpec) []helpCommandSpec {
	if len(entries) == 0 {
		return nil
	}
	out := make([]helpCommandSpec, 0, len(entries))
	for _, entry := range entries {
		command := strings.TrimSpace(entry.Command)
		if command == "" {
			continue
		}
		scoped := entry
		switch {
		case strings.HasPrefix(command, "/workspace delete"):
			continue
		case strings.HasPrefix(command, "/workspace"):
			scoped.Summary = groupWorkspaceHelpSummary(command, entry.Summary)
		case strings.HasPrefix(command, "/model plan"), strings.HasPrefix(command, "/model option"):
			continue
		case strings.HasPrefix(command, "/model"):
			scoped.Summary = groupModelHelpSummary(command, entry.Summary)
		case strings.HasPrefix(command, "/effort"):
			scoped.Summary = groupEffortHelpSummary(command, entry.Summary)
		case strings.HasPrefix(command, "/fast"):
			scoped.Summary = groupFastHelpSummary(command, entry.Summary)
		}
		out = append(out, scoped)
	}
	return out
}

func groupWorkspaceHelpSummary(command, fallback string) string {
	switch {
	case command == "/workspace":
		return "打开当前 Bot 在本群内的工作区菜单。"
	case command == "/workspace list", command == "/workspace choose":
		return "选择当前 Bot 在本群内使用的本机工作区。"
	case strings.HasPrefix(command, "/workspace use"):
		return "设置当前 Bot 在本群内使用的本机工作区。"
	case strings.HasPrefix(command, "/workspace new"):
		return "创建本机工作区，并设置为当前 Bot 在本群内使用。"
	case strings.HasPrefix(command, "/workspace clone"):
		return "从 Git 仓库创建本机工作区，并设置为当前 Bot 在本群内使用。"
	case strings.HasPrefix(command, "/workspace sandbox"):
		return "设置当前 Bot 在本群内的 sandbox 覆盖。"
	case strings.HasPrefix(command, "/workspace policy"):
		return "设置当前 Bot 在本群内的 approval policy 覆盖。"
	case strings.HasPrefix(command, "/workspace multiagent"):
		return "设置当前 Bot 在本群内的 multi-agent mode 覆盖。"
	case strings.HasPrefix(command, "/workspace permissions"):
		return "设置当前 Bot 在本群内的 Claude 权限覆盖。"
	default:
		return fallback
	}
}

func groupModelHelpSummary(command, fallback string) string {
	switch {
	case command == "/model":
		return "打开当前 Bot 在本群内的模型与推理强度配置。"
	case strings.HasPrefix(command, "/model set"):
		return "设置当前 Bot 在本群内的 model 覆盖。"
	case strings.HasPrefix(command, "/model effort"):
		return "设置当前 Bot 在本群内的推理强度覆盖。"
	default:
		return fallback
	}
}

func groupEffortHelpSummary(command, fallback string) string {
	switch {
	case command == "/effort":
		return "打开当前 Bot 在本群内的模型与推理强度配置。"
	case strings.HasPrefix(command, "/effort"):
		return "设置当前 Bot 在本群内的推理强度覆盖。"
	default:
		return fallback
	}
}

func groupFastHelpSummary(command, fallback string) string {
	switch {
	case command == "/fast", strings.HasPrefix(command, "/fast "):
		return "设置当前 Bot 在本群内的响应速度覆盖。"
	default:
		return fallback
	}
}
