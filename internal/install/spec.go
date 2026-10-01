package install

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"feidex/internal/codexcli"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
)

const selfUpdateTargetLatest = "latest"

// Spec holds everything that differs between CLI backends. The Manager
// implements the rest of the probe/upgrade flow once.
type Spec struct {
	// Name is the capitalized backend name used in user-facing messages.
	Name string
	// CommandName is the default CLI command name.
	CommandName string
	// PackageName is the npm package that provides the CLI.
	PackageName string
	// ParseVersion extracts a version from `<command> --version` output.
	ParseVersion func(stdout string) string
	// UserAgent resolves the User-Agent sent to the npm registry. It may run
	// the CLI, so it takes the manager and a context.
	UserAgent func(m *Manager, ctx context.Context) (string, error)
}

// Claude returns the spec for the Claude Code CLI.
func Claude() Spec {
	return Spec{
		Name:         "Claude",
		CommandName:  "claude",
		PackageName:  "@anthropic-ai/claude-code",
		ParseVersion: parseSemverVersion,
		UserAgent: func(m *Manager, ctx context.Context) (string, error) {
			version, err := m.currentVersion(ctx)
			if err != nil {
				return "", err
			}
			return claudeUserAgent(version), nil
		},
	}
}

// Codex returns the spec for the Codex CLI.
func Codex() Spec {
	return Spec{
		Name:         "Codex",
		CommandName:  "codex",
		PackageName:  "@openai/codex",
		ParseVersion: codexcli.ParseVersion,
		UserAgent: func(m *Manager, ctx context.Context) (string, error) {
			return codexUserAgentLookup(ctx, m.commandOrDefault())
		},
	}
}

// codexUserAgentLookup is a seam: tests replace it to avoid starting a real
// app-server process.
var codexUserAgentLookup = func(ctx context.Context, command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		command = "codex"
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client := codexrpc.New(config.CodexConfig{Command: command})
	if err := client.Start(probeCtx, false); err != nil {
		return "", err
	}
	defer func() { _ = client.Close() }()
	userAgent := strings.TrimSpace(client.UserAgent())
	if userAgent == "" {
		return "", fmt.Errorf("codex initialize response missing userAgent")
	}
	return userAgent, nil
}

func claudeUserAgent(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	return "claude-cli/" + version + " (external, cli)"
}

var versionPattern = regexp.MustCompile(`\bv?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?\b`)

// parseSemverVersion pulls the first semver-looking token out of raw CLI output.
func parseSemverVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	version := versionPattern.FindString(raw)
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}
