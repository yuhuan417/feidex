package app

import (
	appapproval "feidex/internal/app/approval"
	"fmt"
	"sort"
	"strings"
)

type permissionSummarySection struct {
	Title string
	Lines []string
}

func renderPermissionsApprovalBody(params map[string]any) string {
	lines := []string{"权限审批"}
	if reason := strings.TrimSpace(stringValue(params["reason"])); reason != "" {
		lines = append(lines, "说明:", reason)
	}
	permissions, _ := params["permissions"].(map[string]any)
	sections := append(permissionRequestSections(params), permissionSummarySections(permissions)...)
	if len(sections) > 0 {
		if len(lines) > 1 {
			lines = append(lines, "")
		}
		for i, section := range sections {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, section.Title+":")
			for _, line := range section.Lines {
				lines = append(lines, "- "+line)
			}
		}
	}
	if len(sections) == 0 {
		if rendered := strings.TrimSpace(truncate(prettyJSON(permissions), 800)); rendered != "" {
			if len(lines) > 1 {
				lines = append(lines, "")
			}
			lines = append(lines, "权限明细:", markdownCodeBlock(rendered))
		}
	}
	if len(sections) == 0 && len(lines) == 1 {
		if requestSummary := strings.TrimSpace(appapproval.TruncatedRequestJSON(params)); requestSummary != "" {
			lines = append(lines, markdownCodeBlock(requestSummary))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func permissionRequestSections(params map[string]any) []permissionSummarySection {
	if len(params) == 0 {
		return nil
	}
	lines := summarizePermissionRequest(params)
	if len(lines) == 0 {
		return nil
	}
	return []permissionSummarySection{{Title: "工具请求", Lines: lines}}
}

func permissionSummarySections(permissions map[string]any) []permissionSummarySection {
	permissions = filterPermissionSummaryPayload(permissions)
	if len(permissions) == 0 {
		return nil
	}
	sections := []permissionSummarySection{}
	if summary := summarizePermissionMetadata(permissions); len(summary) > 0 {
		sections = append(sections, permissionSummarySection{Title: "权限摘要", Lines: summary})
	}
	if fileSystem := summarizePermissionFileSystem(permissions); len(fileSystem) > 0 {
		sections = append(sections, permissionSummarySection{Title: "fileSystem", Lines: fileSystem})
	}
	if network := summarizePermissionNetwork(permissions); len(network) > 0 {
		sections = append(sections, permissionSummarySection{Title: "network", Lines: network})
	}
	if len(sections) > 0 {
		return sections
	}
	if fallback := summarizePermissionsFallback(permissions); len(fallback) > 0 {
		return []permissionSummarySection{{Title: "权限摘要", Lines: fallback}}
	}
	return nil
}

func summarizePermissions(permissions map[string]any) []string {
	permissions = filterPermissionSummaryPayload(permissions)
	if summary := summarizePermissionMetadata(permissions); len(summary) > 0 {
		return summary
	}
	return summarizePermissionsFallback(permissions)
}

func summarizePermissionRequest(params map[string]any) []string {
	if len(params) == 0 {
		return nil
	}
	lines := []string{}
	seen := map[string]struct{}{}
	add := func(label string, values ...string) {
		if _, ok := seen[label]; ok {
			return
		}
		value := strings.TrimSpace(firstNonEmpty(values...))
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		seen[label] = struct{}{}
		lines = append(lines, label+": `"+strings.ReplaceAll(truncate(value, 180), "`", "'")+"`")
	}

	tool := strings.TrimSpace(firstNonEmpty(
		stringValue(params["tool"]),
		stringValue(params["toolName"]),
		stringValue(params["tool_name"]),
	))
	if tool == "" {
		if permissions, ok := params["permissions"].(map[string]any); ok {
			tool = strings.TrimSpace(firstNonEmpty(
				stringValue(permissions["tool"]),
				stringValue(permissions["toolName"]),
				stringValue(permissions["tool_name"]),
			))
		}
	}
	add("tool", tool)

	if toolInput, ok := params["tool_input"].(map[string]any); ok && len(toolInput) > 0 {
		add("url", firstNonEmpty(
			stringValue(toolInput["url"]),
			stringValuesSummary(toolInput["urls"], 4),
		))
		add("query", firstNonEmpty(
			stringValue(toolInput["query"]),
			stringValue(toolInput["searchTerm"]),
			stringValue(toolInput["search_term"]),
		))
		add("prompt", stringValue(toolInput["prompt"]))
		add("description", stringValue(toolInput["description"]))
		add("command", firstNonEmpty(
			stringValue(toolInput["command"]),
			stringValue(toolInput["cmd"]),
		))
		add("path", firstNonEmpty(
			stringValue(toolInput["path"]),
			stringValue(toolInput["file_path"]),
			stringValue(toolInput["notebook_path"]),
		))
		add("cwd", stringValue(toolInput["cwd"]))
		add("pattern", firstNonEmpty(
			stringValue(toolInput["pattern"]),
			stringValue(toolInput["regex"]),
		))
		add("glob", stringValue(toolInput["glob"]))
		add("domain", firstNonEmpty(
			stringValue(toolInput["domain"]),
			stringValuesSummary(toolInput["domains"], 4),
		))
		add("target", firstNonEmpty(
			stringValue(toolInput["target"]),
			stringValue(toolInput["resource"]),
			stringValue(toolInput["file"]),
		))
		for _, item := range flattenPermissionScalars("", toolInput, 0) {
			parts := strings.SplitN(item, " = ", 2)
			if len(parts) != 2 {
				continue
			}
			label := strings.TrimSpace(parts[0])
			if label == "" {
				continue
			}
			add(label, strings.Trim(parts[1], "`"))
			if len(lines) >= 7 {
				break
			}
		}
	}

	add("blocked_path", firstNonEmpty(
		stringValue(params["blockedPath"]),
		stringValue(params["blocked_path"]),
		func() string {
			if permissions, ok := params["permissions"].(map[string]any); ok {
				return firstNonEmpty(stringValue(permissions["blockedPath"]), stringValue(permissions["blocked_path"]))
			}
			return ""
		}(),
	))

	return lines
}

func summarizePermissionMetadata(permissions map[string]any) []string {
	if len(permissions) == 0 {
		return nil
	}
	lines := []string{}
	add := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		lines = append(lines, label+": "+value)
	}

	if mode := strings.TrimSpace(firstNonEmpty(
		stringValue(permissions["mode"]),
		stringValue(permissions["access"]),
		stringValue(permissions["level"]),
	)); mode != "" {
		add("mode", "`"+strings.ReplaceAll(mode, "`", "'")+"`")
	}
	if scope := strings.TrimSpace(firstNonEmpty(
		stringValue(permissions["scope"]),
		stringValue(permissions["grant_scope"]),
		stringValue(permissions["session_scope"]),
	)); scope != "" {
		add("scope", "`"+strings.ReplaceAll(scope, "`", "'")+"`")
	}
	if sandbox := extractPermissionLabelledValue(permissions, "sandbox", "sandboxMode", "sandbox_mode", "type"); sandbox != "" {
		add("sandbox", "`"+strings.ReplaceAll(sandbox, "`", "'")+"`")
	}
	return lines
}

func summarizePermissionFileSystem(permissions map[string]any) []string {
	if len(permissions) == 0 {
		return nil
	}
	lines := []string{}
	if fileSystem, ok := permissions["fileSystem"].(map[string]any); ok && len(fileSystem) > 0 {
		if read := permissionPathsFromValue(fileSystem["read"]); len(read) > 0 {
			lines = append(lines, "read: "+formatPermissionPathList(read))
		}
		if write := permissionPathsFromValue(fileSystem["write"]); len(write) > 0 {
			lines = append(lines, "write: "+formatPermissionPathList(write))
		}
		if len(lines) > 0 {
			return lines
		}
		if flat := summarizePermissionsFallback(fileSystem); len(flat) > 0 {
			return flat
		}
	}
	if legacy := collectPermissionPaths(permissions); len(legacy) > 0 {
		return []string{"paths: " + formatPermissionPathList(legacy)}
	}
	return nil
}

func summarizePermissionNetwork(permissions map[string]any) []string {
	if len(permissions) == 0 {
		return nil
	}
	if network, ok := permissions["network"].(map[string]any); ok && len(network) > 0 {
		if enabled, ok := extractPermissionBool(network, "enabled"); ok {
			return []string{"enabled: " + permissionBoolLabel(enabled)}
		}
		if flat := summarizePermissionsFallback(network); len(flat) > 0 {
			return flat
		}
	}
	if enabled, ok := extractPermissionBool(permissions, "network", "networkAccess", "network_access", "allowNetwork", "allow_network"); ok {
		return []string{"enabled: " + permissionBoolLabel(enabled)}
	}
	return nil
}

func permissionPathsFromValue(value any) []string {
	return collectPermissionPaths(map[string]any{"paths": value})
}

func formatPermissionPathList(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	const maxPaths = 6
	shown := paths
	if len(shown) > maxPaths {
		shown = shown[:maxPaths]
	}
	value := "`" + strings.Join(shown, "`, `") + "`"
	if len(paths) > maxPaths {
		value += fmt.Sprintf(" 等 %d 项", len(paths))
	}
	return value
}

func permissionBoolLabel(enabled bool) string {
	if enabled {
		return "允许"
	}
	return "禁止"
}

func summarizePermissionsFallback(permissions map[string]any) []string {
	permissions = filterPermissionSummaryPayload(permissions)
	flat := flattenPermissionScalars("", permissions, 0)
	if len(flat) == 0 {
		return nil
	}
	sort.Strings(flat)
	if len(flat) > 8 {
		flat = flat[:8]
	}
	return flat
}

func extractPermissionLabelledValue(root map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringValue(root[key])); value != "" {
			return value
		}
	}
	for _, key := range keys {
		if nested, ok := root[key].(map[string]any); ok {
			if value := strings.TrimSpace(firstNonEmpty(
				stringValue(nested["type"]),
				stringValue(nested["mode"]),
				stringValue(nested["value"]),
			)); value != "" {
				return value
			}
		}
	}
	return ""
}

func extractPermissionBool(root map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if value, ok := boolValue(root[key]); ok {
			return value, true
		}
	}
	return false, false
}

func collectPermissionPaths(root map[string]any) []string {
	values := []string{}
	seen := map[string]struct{}{}
	var add func(string)
	add = func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		values = append(values, path)
	}
	var walk func(any, int)
	walk = func(current any, depth int) {
		if current == nil || depth > 3 {
			return
		}
		switch x := current.(type) {
		case string:
			add(x)
		case []any:
			for _, item := range x {
				walk(item, depth+1)
			}
		case map[string]any:
			for _, key := range []string{"paths", "roots", "writable_roots", "writableRoots", "allowed_paths", "allowedPaths"} {
				if nested, ok := x[key]; ok {
					walk(nested, depth+1)
				}
			}
		}
	}
	walk(root, 0)
	sort.Strings(values)
	return values
}

func boolValue(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "yes", "1", "allow", "allowed":
			return true, true
		case "false", "no", "0", "deny", "denied":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func flattenPermissionScalars(prefix string, value any, depth int) []string {
	if depth > 3 || value == nil {
		return nil
	}
	lines := []string{}
	switch x := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			nextPrefix := key
			if prefix != "" {
				nextPrefix = prefix + "." + key
			}
			lines = append(lines, flattenPermissionScalars(nextPrefix, x[key], depth+1)...)
		}
	case []any:
		for i, item := range x {
			lines = append(lines, flattenPermissionScalars(fmt.Sprintf("%s[%d]", prefix, i), item, depth+1)...)
		}
	case string:
		if trimmed := strings.TrimSpace(x); trimmed != "" {
			lines = append(lines, prefix+" = `"+strings.ReplaceAll(trimmed, "`", "'")+"`")
		}
	case bool:
		lines = append(lines, fmt.Sprintf("%s = %t", prefix, x))
	case float64:
		lines = append(lines, fmt.Sprintf("%s = %v", prefix, x))
	}
	return lines
}

func filterPermissionSummaryPayload(permissions map[string]any) map[string]any {
	if len(permissions) == 0 {
		return nil
	}
	filtered := map[string]any{}
	for key, value := range permissions {
		switch key {
		case "tool", "toolName", "tool_name", "blockedPath", "blocked_path":
			continue
		default:
			filtered[key] = value
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func stringValuesSummary(value any, limit int) string {
	values := collectStringValues(value, limit)
	if len(values) == 0 {
		return ""
	}
	if limit > 0 && len(values) > limit {
		values = values[:limit]
	}
	return strings.Join(values, ", ")
}

func collectStringValues(value any, limit int) []string {
	out := []string{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		out = append(out, v)
	}
	var walk func(any)
	walk = func(current any) {
		if current == nil {
			return
		}
		if limit > 0 && len(out) >= limit {
			return
		}
		switch x := current.(type) {
		case string:
			add(x)
		case []string:
			for _, item := range x {
				add(item)
				if limit > 0 && len(out) >= limit {
					return
				}
			}
		case []any:
			for _, item := range x {
				walk(item)
				if limit > 0 && len(out) >= limit {
					return
				}
			}
		}
	}
	walk(value)
	return out
}
