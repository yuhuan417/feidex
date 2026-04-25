package workspacecmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/config"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

func formatTurnElapsedLine(d time.Duration) string {
	seconds := int(d.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("elapsed: %ds", seconds)
	}
	minutes := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("elapsed: %dm%ds", minutes, secs)
}

func markdownCodeBlock(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	return "```\n" + content + "\n```"
}

func sameWorkspaceCWD(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func defaultWorkspaceCloneRoot(_ *config.Workspace) string {
	return "/"
}

func submenuLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "›"
	}
	return label + " ›"
}

func commandLabel(label, slash string) string {
	label = strings.TrimSpace(label)
	slash = strings.TrimSpace(slash)
	if label == "" {
		return slash
	}
	if slash == "" {
		return label
	}
	return label + " " + slash
}

func submenuCommandLabel(label, slash string) string {
	return submenuLabel(commandLabel(label, slash))
}

func defaultWorkspaceCloneParent(ws *config.Workspace, cfgPath string) string {
	if ws != nil && strings.TrimSpace(ws.Cwd) != "" {
		return filepath.Dir(strings.TrimSpace(ws.Cwd))
	}
	if strings.TrimSpace(cfgPath) != "" {
		return filepath.Dir(strings.TrimSpace(cfgPath))
	}
	return "."
}
