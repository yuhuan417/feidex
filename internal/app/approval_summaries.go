package app

import (
	"fmt"
	"sort"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func renderCommandApprovalBody(params map[string]any) string {
	lines := []string{"命令审批"}
	if command := strings.TrimSpace(firstNonEmpty(
		stringValue(params["command"]),
		stringValue(params["commandLine"]),
		stringValue(params["command_line"]),
	)); command != "" {
		lines = append(lines, markdownCodeBlock(command))
	}
	if cwd := strings.TrimSpace(firstNonEmpty(
		stringValue(params["cwd"]),
		stringValue(params["workingDirectory"]),
		stringValue(params["working_directory"]),
	)); cwd != "" {
		lines = append(lines, "工作目录: `"+strings.ReplaceAll(cwd, "`", "'")+"`")
	}
	if reason := strings.TrimSpace(stringValue(params["reason"])); reason != "" {
		if len(lines) > 1 {
			lines = append(lines, "")
		}
		lines = append(lines, "说明:", reason)
	}
	if len(lines) == 1 {
		if summary := strings.TrimSpace(truncatedApprovalRequestJSON(params)); summary != "" {
			lines = append(lines, markdownCodeBlock(summary))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func renderPermissionsApprovalBody(params map[string]any) string {
	lines := []string{"权限审批"}
	if reason := strings.TrimSpace(stringValue(params["reason"])); reason != "" {
		lines = append(lines, "说明:", reason)
	}
	permissions, _ := params["permissions"].(map[string]any)
	summary := summarizePermissions(permissions)
	if len(summary) > 0 {
		if len(lines) > 1 {
			lines = append(lines, "")
		}
		lines = append(lines, "权限摘要:")
		for _, line := range summary {
			lines = append(lines, "- "+line)
		}
	}
	if len(summary) == 0 {
		if rendered := strings.TrimSpace(truncate(prettyJSON(permissions), 800)); rendered != "" {
			if len(lines) > 1 {
				lines = append(lines, "")
			}
			lines = append(lines, "权限明细:", markdownCodeBlock(rendered))
		}
	}
	if len(summary) == 0 && len(lines) == 1 {
		if requestSummary := strings.TrimSpace(truncatedApprovalRequestJSON(params)); requestSummary != "" {
			lines = append(lines, markdownCodeBlock(requestSummary))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizePermissions(permissions map[string]any) []string {
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
	if network, ok := extractPermissionBool(permissions, "network", "networkAccess", "network_access", "allowNetwork", "allow_network"); ok {
		if network {
			add("network", "允许")
		} else {
			add("network", "禁止")
		}
	}
	if writable := collectPermissionPaths(permissions); len(writable) > 0 {
		const maxPaths = 6
		shown := writable
		if len(shown) > maxPaths {
			shown = shown[:maxPaths]
		}
		value := "`" + strings.Join(shown, "`, `") + "`"
		if len(writable) > maxPaths {
			value += fmt.Sprintf(" 等 %d 项", len(writable))
		}
		add("paths", value)
	}
	if len(lines) > 0 {
		return lines
	}
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

type approvalFileEntry struct {
	Path string
	Kind string
}

func renderFileApprovalBody(params map[string]any) string {
	lines := []string{"文件变更审批"}
	entries := collectFileApprovalEntries(params)
	if len(entries) > 0 {
		lines = append(lines, fmt.Sprintf("%d 个文件：", len(entries)))
		const maxEntries = 8
		for i, entry := range entries {
			if i >= maxEntries {
				lines = append(lines, fmt.Sprintf("- 还有 %d 个文件未展开", len(entries)-maxEntries))
				break
			}
			line := entry.Path
			if strings.TrimSpace(entry.Kind) != "" {
				line += " (" + entry.Kind + ")"
			}
			lines = append(lines, "- "+line)
		}
	}
	if reason := strings.TrimSpace(stringValue(params["reason"])); reason != "" {
		if len(lines) > 1 {
			lines = append(lines, "")
		}
		lines = append(lines, "说明:", reason)
	}
	if len(entries) == 0 {
		if summary := strings.TrimSpace(truncatedApprovalRequestJSON(params)); summary != "" {
			if len(lines) > 1 {
				lines = append(lines, "")
			}
			lines = append(lines, "请求摘要:", markdownCodeBlock(summary))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func collectFileApprovalEntries(value any) []approvalFileEntry {
	out := []approvalFileEntry{}
	seen := map[string]struct{}{}
	var add func(approvalFileEntry)
	add = func(entry approvalFileEntry) {
		entry.Path = strings.TrimSpace(entry.Path)
		entry.Kind = strings.TrimSpace(entry.Kind)
		if entry.Path == "" {
			return
		}
		key := entry.Path + "\x00" + entry.Kind
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, entry)
	}
	var walk func(any, int)
	walk = func(current any, depth int) {
		if depth > 3 || current == nil {
			return
		}
		switch x := current.(type) {
		case string:
			add(approvalFileEntry{Path: x})
		case []any:
			for _, item := range x {
				add(parseApprovalFileEntry(item))
			}
		case map[string]any:
			for _, key := range []string{"changes", "fileChanges", "file_changes", "files", "paths", "filePaths", "file_paths"} {
				if arr, ok := x[key].([]any); ok {
					walk(arr, depth+1)
				}
			}
			for _, key := range []string{"path", "file", "filePath", "file_path", "targetPath", "target_path"} {
				if path := strings.TrimSpace(stringValue(x[key])); path != "" {
					add(parseApprovalFileEntry(x))
					break
				}
			}
			for _, key := range []string{"item", "payload", "change", "fileChange", "file_change", "details", "result"} {
				if nested, ok := x[key]; ok {
					walk(nested, depth+1)
				}
			}
		}
	}
	walk(value, 0)
	return out
}

func parseApprovalFileEntry(value any) approvalFileEntry {
	switch x := value.(type) {
	case string:
		return approvalFileEntry{Path: strings.TrimSpace(x)}
	case map[string]any:
		oldPath := strings.TrimSpace(firstNonEmpty(stringValue(x["oldPath"]), stringValue(x["old_path"])))
		newPath := strings.TrimSpace(firstNonEmpty(stringValue(x["newPath"]), stringValue(x["new_path"])))
		path := strings.TrimSpace(firstNonEmpty(
			stringValue(x["path"]),
			stringValue(x["file"]),
			stringValue(x["filePath"]),
			stringValue(x["file_path"]),
			stringValue(x["targetPath"]),
			stringValue(x["target_path"]),
			stringValue(x["name"]),
		))
		if oldPath != "" && newPath != "" && oldPath != newPath {
			path = oldPath + " -> " + newPath
		}
		kind := strings.TrimSpace(firstNonEmpty(
			stringValue(x["kind"]),
			stringValue(x["changeType"]),
			stringValue(x["change_type"]),
			stringValue(x["op"]),
			stringValue(x["action"]),
			stringValue(x["status"]),
			stringValue(x["type"]),
		))
		return approvalFileEntry{Path: path, Kind: kind}
	default:
		return approvalFileEntry{}
	}
}

func truncatedApprovalRequestJSON(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	trimmed := map[string]any{}
	for key, value := range params {
		switch key {
		case "threadId", "turnId", "itemId", "reason":
			continue
		default:
			trimmed[key] = value
		}
	}
	if len(trimmed) == 0 {
		return ""
	}
	return truncate(prettyJSON(trimmed), 800)
}

func approvalButtons(kind, requestKey string) []feishu.Button {
	return []feishu.Button{
		{Text: "允许一次", Type: "primary", Value: map[string]any{"action": "approval." + kind + ".accept", "request_id": requestKey}},
		{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval." + kind + ".accept_session", "request_id": requestKey}},
		{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval." + kind + ".decline", "request_id": requestKey}},
	}
}

func (a *App) renderApprovalCard(_ string, _ *state.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
	return a.feishu.SimpleStatusCard(title, color, strings.TrimSpace(body), buttons)
}
