package app

import (
	"fmt"
	"reflect"
	"strings"
)

type approvalFileEntry struct {
	Path string
	Kind string
}

func renderFileApprovalBody(params map[string]any) string {
	return renderFileApprovalBodyWithWorkspace(params, "")
}

func renderFileApprovalBodyWithWorkspace(params map[string]any, workspaceCwd string) string {
	lines := []string{"文件变更审批"}
	entries := collectFileApprovalEntriesWithWorkspace(params, workspaceCwd)
	summaryLines := []string{}
	if len(entries) > 0 {
		summaryLines = append(summaryLines, fmt.Sprintf("- 文件数: %d", len(entries)))
	}
	if grantRoot := fileApprovalGrantRootSummary(params["grantRoot"], workspaceCwd); grantRoot != "" {
		summaryLines = append(summaryLines, "- 授权根目录: "+grantRoot)
	}
	if len(summaryLines) > 0 {
		lines = append(lines, "", "变更摘要:")
		lines = append(lines, summaryLines...)
	}
	if len(entries) > 0 {
		lines = append(lines, "", "文件列表:")
		const maxEntries = 8
		for i, entry := range entries {
			if i >= maxEntries {
				lines = append(lines, fmt.Sprintf("- 还有 %d 个文件未展开", len(entries)-maxEntries))
				break
			}
			lines = append(lines, renderApprovalFileEntryLine(entry))
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

func renderApprovalFileEntryLine(entry approvalFileEntry) string {
	path := strings.TrimSpace(entry.Path)
	if path == "" {
		return ""
	}
	path = "`" + strings.ReplaceAll(path, "`", "'") + "`"
	if kind := approvalFileKindLabel(entry.Kind); kind != "" {
		return "- " + path + " · " + kind
	}
	return "- " + path
}

func approvalFileKindLabel(kind string) string {
	kind = strings.TrimSpace(kind)
	switch strings.ToLower(kind) {
	case "":
		return ""
	case "write", "create", "created", "new":
		return "写入"
	case "add", "added":
		return "新增"
	case "edit", "edited", "modify", "modified", "update", "updated", "notebookedit":
		return "修改"
	case "delete", "deleted", "remove", "removed":
		return "删除"
	case "rename", "renamed", "move", "moved":
		return "重命名"
	default:
		return kind
	}
}

func fileApprovalGrantRootSummary(value any, workspaceCwd string) string {
	switch x := value.(type) {
	case nil:
		return ""
	case string:
		path := strings.TrimSpace(renderWorkspaceDisplayPath(x, workspaceCwd))
		if path == "" {
			return ""
		}
		return "`" + strings.ReplaceAll(path, "`", "'") + "`"
	case map[string]any:
		path := strings.TrimSpace(firstNonEmpty(
			stringValue(x["path"]),
			stringValue(x["root"]),
			stringValue(x["value"]),
			stringValue(x["grantRoot"]),
			stringValue(x["grant_root"]),
		))
		if path != "" {
			path = renderWorkspaceDisplayPath(path, workspaceCwd)
			return "`" + strings.ReplaceAll(path, "`", "'") + "`"
		}
		if rendered := strings.TrimSpace(truncate(prettyJSON(x), 300)); rendered != "" {
			return markdownCodeBlock(rendered)
		}
	case bool:
		if x {
			return "允许"
		}
		return "禁止"
	}
	if enabled, ok := boolValue(value); ok {
		if enabled {
			return "允许"
		}
		return "禁止"
	}
	if rendered := strings.TrimSpace(stringValue(value)); rendered != "" {
		return "`" + strings.ReplaceAll(rendered, "`", "'") + "`"
	}
	return ""
}

func collectFileApprovalEntries(value any) []approvalFileEntry {
	return collectFileApprovalEntriesWithWorkspace(value, "")
}

func collectFileApprovalEntriesWithWorkspace(value any, workspaceCwd string) []approvalFileEntry {
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
			add(approvalFileEntry{Path: renderWorkspaceDisplayPath(x, workspaceCwd)})
		case map[string]any:
			for _, key := range []string{"changes", "fileChanges", "file_changes", "files", "paths", "filePaths", "file_paths"} {
				if nested, ok := x[key]; ok {
					walk(nested, depth+1)
				}
			}
			for _, key := range []string{"path", "file", "filePath", "file_path", "targetPath", "target_path", "oldPath", "old_path", "newPath", "new_path"} {
				if path := strings.TrimSpace(stringValue(x[key])); path != "" {
					add(parseApprovalFileEntryWithWorkspace(x, workspaceCwd))
					break
				}
			}
			for _, key := range []string{"item", "payload", "change", "fileChange", "file_change", "details", "result"} {
				if nested, ok := x[key]; ok {
					walk(nested, depth+1)
				}
			}
		default:
			rv := reflect.ValueOf(current)
			if !rv.IsValid() {
				return
			}
			switch rv.Kind() {
			case reflect.Slice, reflect.Array:
				for i := 0; i < rv.Len(); i++ {
					walk(rv.Index(i).Interface(), depth+1)
				}
			}
		}
	}
	walk(value, 0)
	return out
}

func parseApprovalFileEntry(value any) approvalFileEntry {
	return parseApprovalFileEntryWithWorkspace(value, "")
}

func parseApprovalFileEntryWithWorkspace(value any, workspaceCwd string) approvalFileEntry {
	switch x := value.(type) {
	case string:
		return approvalFileEntry{Path: renderWorkspaceDisplayPath(x, workspaceCwd)}
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
			path = renderWorkspaceDisplayPath(oldPath, workspaceCwd) + " -> " + renderWorkspaceDisplayPath(newPath, workspaceCwd)
		} else {
			path = renderWorkspaceDisplayPath(path, workspaceCwd)
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
