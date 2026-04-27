// Package commandmatch provides pure command-matching functions extracted
// from the app package. These functions operate on string slices and have
// no dependency on *App.
package commandmatch

import (
	"fmt"
	"strconv"
	"strings"

	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/release"
)

func ExactCommand(fields []string) bool {
	return len(fields) == 1
}

func ExactOrSingleArgCommand(fields []string, allowed ...string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 2 {
		return false
	}
	return CommandArgInSet(fields[1], allowed...)
}

func MatchBackendCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) == 2 && strings.TrimSpace(fields[1]) == "retry" {
		return true
	}
	if len(fields) != 3 || strings.TrimSpace(fields[1]) != "retry" {
		return false
	}
	return CommandArgInSet(fields[2], "status", "on", "off")
}

func CommandArgInSet(value string, allowed ...string) bool {
	value = strings.TrimSpace(value)
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func MatchReviewCommand(fields []string) bool {
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

func MatchHistoryCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 3 || strings.TrimSpace(fields[1]) != "detail" {
		return false
	}
	value, err := strconv.Atoi(strings.TrimSpace(fields[2]))
	return err == nil && value > 0
}

func MatchModelCommand(fields []string) bool {
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

func MatchEffortCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	return len(fields) == 2 && strings.TrimSpace(fields[1]) != ""
}

func MatchThreadCommand(fields []string) bool {
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

func MatchSessionCommand(fields []string) bool {
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

func MatchUpgradeCommand(fields []string) bool {
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
		_, err := NormalizeUpgradeVersion(fields[1])
		return err == nil
	}
}

func MatchCodexCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 2 {
		return false
	}
	return CommandArgInSet(fields[1], "check", "upgrade", "restart")
}

func MatchClaudeCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 2 {
		return false
	}
	return CommandArgInSet(fields[1], "check", "upgrade", "restart")
}

func MatchWorkspaceCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	switch strings.TrimSpace(fields[1]) {
	case "list", "new", "choose":
		return len(fields) == 2
	case "delete":
		return len(fields) == 2 || len(fields) == 3
	case "sandbox", "policy":
		return len(fields) == 2 || len(fields) == 3
	case "clone":
		_, _, _, err := appworkspace.ParseCloneArgs(fields[1:])
		return err == nil
	case "use":
		return len(fields) == 3
	default:
		return false
	}
}

func MatchClaudeWorkspaceCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	switch strings.TrimSpace(fields[1]) {
	case "list", "new", "choose":
		return len(fields) == 2
	case "delete":
		return len(fields) == 2 || len(fields) == 3
	case "clone":
		_, _, _, err := appworkspace.ParseCloneArgs(fields[1:])
		return err == nil
	case "use":
		return len(fields) == 3
	case "permissions":
		return len(fields) == 2 || len(fields) == 3
	default:
		return false
	}
}

func NormalizeUpgradeVersion(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("missing version")
	}
	if _, err := release.ParseVersion(raw); err != nil {
		return "", err
	}
	return "v" + strings.TrimPrefix(raw, "v"), nil
}
