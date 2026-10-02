// Package debugview provides pure formatting and rendering functions for
// debug log display in Feishu cards.
package debugview

import (
	"fmt"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/logcontrol"
)

// RuntimeLogLevelText returns the name of the current runtime slog level.
func RuntimeLogLevelText() string {
	return logcontrol.CurrentName()
}

// DesiredDebugEnabled parses the desired debug state from command args.
// With no args, it toggles the current state. With one arg ("on"/"off"),
// it uses that value.
func DesiredDebugEnabled(args []string) (bool, error) {
	if len(args) == 0 {
		return !logcontrol.DebugEnabled(), nil
	}
	if len(args) > 1 {
		return false, fmt.Errorf("usage: /debug | /debug on | /debug off")
	}
	if enabled, ok := logcontrol.ToggleArgEnabled(args[0]); ok {
		return enabled, nil
	}
	return false, fmt.Errorf("usage: /debug | /debug on | /debug off")
}

// DebugUserAllowed checks whether a userID is present in the allowFrom list.
// A wildcard "*" in allowFrom matches any user.
func DebugUserAllowed(userID string, allowFrom []string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	for _, item := range allowFrom {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if item == "*" || item == userID {
			return true
		}
	}
	return false
}

// CompactDebugLogText compacts log lines to fit within maxChars, keeping
// the most recent lines. Returns the text, number of lines shown, and
// whether the output was truncated.
func CompactDebugLogText(lines []string, maxChars int) (string, int, bool) {
	if len(lines) == 0 {
		return "", 0, false
	}
	if maxChars <= 0 {
		return strings.Join(lines, "\n"), len(lines), false
	}
	selected := make([]string, 0, len(lines))
	size := 0
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		extra := len(line)
		if len(selected) > 0 {
			extra++
		}
		if size+extra > maxChars && len(selected) > 0 {
			break
		}
		if size+extra > maxChars {
			if maxChars > 3 && len(line) > maxChars-3 {
				line = "..." + line[len(line)-(maxChars-3):]
			}
			selected = append(selected, line)
			break
		}
		selected = append(selected, line)
		size += extra
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return strings.Join(selected, "\n"), len(selected), len(selected) < len(lines)
}

// DebugLogPlainTextBlock creates a Feishu card div element with plain text.
// If subtle is true, the text uses notation size and grey color.
func DebugLogPlainTextBlock(content string, subtle bool) map[string]any {
	text := map[string]any{
		"tag":     "plain_text",
		"content": strings.TrimSpace(content),
	}
	if subtle {
		text["text_size"] = "notation"
		text["text_color"] = "grey"
	}
	return map[string]any{
		"tag":  "div",
		"text": text,
	}
}

// RenderRuntimeLogLevelValue returns the current log level as a markdown
// inline code string, defaulting to "info" if empty.
func RenderRuntimeLogLevelValue() string {
	level := strings.TrimSpace(RuntimeLogLevelText())
	if level == "" {
		level = "info"
	}
	return "`" + level + "`"
}

// ActionUserID extracts the trimmed UserID from a CardAction.
// Returns "" if action is nil.
func ActionUserID(action *feishu.CardAction) string {
	if action == nil {
		return ""
	}
	return strings.TrimSpace(action.UserID)
}
