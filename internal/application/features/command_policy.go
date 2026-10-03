package features

import (
	"strings"

	"feidex/internal/domain/backend"
)

// HandlesCommand is the single command recognition policy used by input
// routing and menu visibility. Unsupported or malformed commands pass through
// to the configured backend instead of claiming an unavailable local action.
func HandlesCommand(kind, raw string) bool {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return false
	}
	feature, command, ok := FindCommand(fields[0])
	if !ok || !feature.SupportsBackend(kind) {
		return false
	}
	return command.Matches(kind, fields)
}

func (s CommandSpec) Matches(kind string, fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	kind = backend.NormalizeBackend(kind)
	switch s.ID {
	case "menu", "interrupt", "fork", "new", "compact", "download", "usage", "status", "help":
		return ExactCommand(fields)
	case "primary":
		return strings.TrimSpace(fields[0]) == "/primary" && len(fields) <= 2
	case "backend":
		return MatchBackendCommand(fields)
	case "review":
		return MatchReviewCommand(fields)
	case "quiet":
		return ExactOrSingleArgCommand(fields, "config", "verbose", "progress", "normal", "final")
	case "plan":
		return ExactOrSingleArgCommand(fields, "on", "off")
	case "goal":
		return MatchGoalCommand(fields)
	case "history":
		return MatchHistoryCommand(fields)
	case "skills":
		return ExactOrSingleArgCommand(fields, "reload")
	case "thread":
		return kind != backend.BackendClaude && MatchThreadCommand(fields)
	case "session":
		return kind != backend.BackendCodex && MatchSessionCommand(fields)
	case "threads":
		return kind != backend.BackendClaude && ExactCommand(fields)
	case "workspace":
		if kind == backend.BackendClaude {
			return MatchClaudeWorkspaceCommand(fields)
		}
		return MatchWorkspaceCommand(fields)
	case "model":
		return MatchModelCommand(fields) && !(kind == backend.BackendClaude && len(fields) >= 2 && strings.TrimSpace(fields[1]) == "plan")
	case "effort":
		return MatchEffortCommand(fields)
	case "fast":
		return ExactOrSingleArgCommand(fields, "config", "fast", "default", "off", "toggle")
	case "debug":
		return ExactOrSingleArgCommand(fields, "on", "off", "logs")
	case "codex":
		return MatchCodexCommand(fields)
	case "claude":
		return MatchClaudeCommand(fields)
	case "upgrade":
		return MatchUpgradeCommand(fields)
	default:
		return false
	}
}

// CommandAllowedWithoutBackend describes onboarding commands that work before
// the frontend has selected a backend runtime.
func CommandAllowedWithoutBackend(chatType, name string) bool {
	switch strings.TrimSpace(name) {
	case "/backend":
		return true
	case "/workspace", "/primary":
		return chatType == "group"
	default:
		return false
	}
}

func HandlesMessageCommand(kind, chatType, raw string) bool {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 || (fields[0] == "/primary" && strings.TrimSpace(chatType) != "group") {
		return false
	}
	return HandlesCommand(kind, raw)
}
