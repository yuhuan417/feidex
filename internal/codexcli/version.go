package codexcli

import (
	"regexp"
	"strings"
)

// ParseVersion extracts the semver-like version printed by Codex CLI.
func ParseVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	versionPattern := regexp.MustCompile(`\bv?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?\b`)
	version := versionPattern.FindString(raw)
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}
