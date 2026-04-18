package pathdisplay

import (
	"path/filepath"
	"regexp"
	"strings"
)

var lineSuffixRe = regexp.MustCompile(`^(.*?)(?::\d+(?::\d+)?)?$`)
var lineAnchorSuffixRe = regexp.MustCompile(`^(.*?)(?:#L(\d+)(?:C(\d+))?)$`)

// RenderWorkspaceDisplayPath rewrites absolute in-workspace paths to
// workspace-relative display paths while preserving optional :line[:col] suffixes.
func RenderWorkspaceDisplayPath(path, workspaceCwd string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	base, suffix := splitPathLineReference(path)
	base = strings.TrimSpace(base)
	if base == "" {
		return path
	}
	if !filepath.IsAbs(base) {
		return filepath.Clean(base) + suffix
	}
	workspaceCwd = strings.TrimSpace(workspaceCwd)
	if workspaceCwd != "" && pathWithinWorkspace(base, workspaceCwd) {
		if rel, err := filepath.Rel(workspaceCwd, base); err == nil && strings.TrimSpace(rel) != "" {
			return filepath.Clean(rel) + suffix
		}
	}
	return filepath.Clean(base) + suffix
}

func splitPathLineReference(path string) (base, suffix string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ""
	}
	if matched := lineAnchorSuffixRe.FindStringSubmatch(path); len(matched) == 4 {
		base = strings.TrimSpace(matched[1])
		suffix = ":" + matched[2]
		if strings.TrimSpace(matched[3]) != "" {
			suffix += ":" + matched[3]
		}
		return base, suffix
	}
	base = trimLineReferenceSuffix(path)
	return base, strings.TrimPrefix(path, base)
}

func trimLineReferenceSuffix(path string) string {
	match := lineSuffixRe.FindStringSubmatch(path)
	if len(match) == 2 {
		return match[1]
	}
	return path
}

func pathWithinWorkspace(path, workspaceCwd string) bool {
	workspaceCwd = filepath.Clean(workspaceCwd)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(workspaceCwd, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
