package approval

import (
	"fmt"
	"reflect"
	"strings"

	"feidex/internal/app/apputil"
	"feidex/internal/feishu"
	"feidex/internal/pathdisplay"
)

type FileEntry struct {
	Path string
	Kind string
}

func RenderCommandBody(params map[string]any) string {
	lines := []string{"命令审批"}
	if command := strings.TrimSpace(firstNonEmpty(stringValue(params["command"]), stringValue(params["commandLine"]), stringValue(params["command_line"]))); command != "" {
		lines = append(lines, markdownCodeBlock(command))
	}
	if cwd := strings.TrimSpace(firstNonEmpty(stringValue(params["cwd"]), stringValue(params["workingDirectory"]), stringValue(params["working_directory"]))); cwd != "" {
		lines = append(lines, "工作目录: `"+strings.ReplaceAll(cwd, "`", "'")+"`")
	}
	if target := strings.TrimSpace(CommandNetworkTarget(params)); target != "" {
		lines = append(lines, "网络访问: `"+strings.ReplaceAll(target, "`", "'")+"`")
	}
	if reason := strings.TrimSpace(stringValue(params["reason"])); reason != "" {
		if len(lines) > 1 {
			lines = append(lines, "")
		}
		lines = append(lines, "说明:", reason)
	}
	if len(lines) == 1 {
		if summary := strings.TrimSpace(TruncatedRequestJSON(params)); summary != "" {
			lines = append(lines, markdownCodeBlock(summary))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func Buttons(kind, requestKey string, requestPayload ...map[string]any) []feishu.Button {
	switch NormalizeKind(kind) {
	case KindCommand:
		return []feishu.Button{
			{Text: "允许一次", Type: "primary", Value: map[string]any{"action": "approval.command.accept", "request_id": requestKey}},
			{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval.command.accept_session", "request_id": requestKey}},
			{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval.command.decline", "request_id": requestKey}},
			{Text: "拒绝并中断", Type: "danger", Value: map[string]any{"action": "approval.command.cancel", "request_id": requestKey}},
		}
	case KindFile:
		return []feishu.Button{
			{Text: "允许一次", Type: "primary", Value: map[string]any{"action": "approval.file.accept", "request_id": requestKey}},
			{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval.file.accept_session", "request_id": requestKey}},
			{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval.file.decline", "request_id": requestKey}},
			{Text: "拒绝并中断", Type: "danger", Value: map[string]any{"action": "approval.file.cancel", "request_id": requestKey}},
		}
	case KindPermissions:
		return []feishu.Button{
			{Text: "本次允许", Type: "primary", Value: map[string]any{"action": "approval.permissions.accept_turn", "request_id": requestKey}},
			{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval.permissions.accept_session", "request_id": requestKey}},
			{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval.permissions.decline", "request_id": requestKey}},
		}
	default:
		return []feishu.Button{
			{Text: "允许一次", Type: "primary", Value: map[string]any{"action": "approval." + kind + ".accept", "request_id": requestKey}},
			{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval." + kind + ".accept_session", "request_id": requestKey}},
			{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval." + kind + ".decline", "request_id": requestKey}},
		}
	}
}

func CommandNetworkTarget(params map[string]any) string {
	ctx, _ := params["networkApprovalContext"].(map[string]any)
	if len(ctx) == 0 {
		return ""
	}
	host := strings.TrimSpace(stringValue(ctx["host"]))
	protocol := strings.TrimSpace(stringValue(ctx["protocol"]))
	switch {
	case host == "":
		return ""
	case protocol == "":
		return host
	default:
		return protocol + "://" + host
	}
}

func RenderFileBody(params map[string]any) string { return RenderFileBodyWithWorkspace(params, "") }

func RenderFileBodyWithWorkspace(params map[string]any, workspaceCwd string) string {
	lines := []string{"文件变更审批"}
	entries := CollectFileEntriesWithWorkspace(params, workspaceCwd)
	summaryLines := []string{}
	if len(entries) > 0 {
		summaryLines = append(summaryLines, fmt.Sprintf("- 文件数: %d", len(entries)))
	}
	if grantRoot := FileGrantRootSummary(params["grantRoot"], workspaceCwd); grantRoot != "" {
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
			lines = append(lines, renderFileEntryLine(entry))
		}
	}
	if reason := strings.TrimSpace(stringValue(params["reason"])); reason != "" {
		if len(lines) > 1 {
			lines = append(lines, "")
		}
		lines = append(lines, "说明:", reason)
	}
	if len(entries) == 0 {
		if summary := strings.TrimSpace(TruncatedRequestJSON(params)); summary != "" {
			if len(lines) > 1 {
				lines = append(lines, "")
			}
			lines = append(lines, "请求摘要:", markdownCodeBlock(summary))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func renderFileEntryLine(entry FileEntry) string {
	path := strings.TrimSpace(entry.Path)
	if path == "" {
		return ""
	}
	path = "`" + strings.ReplaceAll(path, "`", "'") + "`"
	if kind := FileKindLabel(entry.Kind); kind != "" {
		return "- " + path + " · " + kind
	}
	return "- " + path
}

func FileKindLabel(kind string) string {
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

func FileGrantRootSummary(value any, workspaceCwd string) string {
	switch x := value.(type) {
	case nil:
		return ""
	case string:
		path := strings.TrimSpace(pathdisplay.RenderWorkspaceDisplayPath(x, workspaceCwd))
		if path == "" {
			return ""
		}
		return "`" + strings.ReplaceAll(path, "`", "'") + "`"
	case map[string]any:
		path := strings.TrimSpace(firstNonEmpty(stringValue(x["path"]), stringValue(x["root"]), stringValue(x["value"]), stringValue(x["grantRoot"]), stringValue(x["grant_root"])))
		if path != "" {
			path = pathdisplay.RenderWorkspaceDisplayPath(path, workspaceCwd)
			return "`" + strings.ReplaceAll(path, "`", "'") + "`"
		}
		if rendered := strings.TrimSpace(apputil.Truncate(prettyJSON(x), 300)); rendered != "" {
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

func CollectFileEntries(value any) []FileEntry { return CollectFileEntriesWithWorkspace(value, "") }

func CollectFileEntriesWithWorkspace(value any, workspaceCwd string) []FileEntry {
	out := []FileEntry{}
	seen := map[string]struct{}{}
	var add func(FileEntry)
	add = func(entry FileEntry) {
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
			add(FileEntry{Path: pathdisplay.RenderWorkspaceDisplayPath(x, workspaceCwd)})
		case map[string]any:
			for _, key := range []string{"changes", "fileChanges", "file_changes", "files", "paths", "filePaths", "file_paths"} {
				if nested, ok := x[key]; ok {
					walk(nested, depth+1)
				}
			}
			for _, key := range []string{"path", "file", "filePath", "file_path", "targetPath", "target_path", "oldPath", "old_path", "newPath", "new_path"} {
				if path := strings.TrimSpace(stringValue(x[key])); path != "" {
					add(ParseFileEntryWithWorkspace(x, workspaceCwd))
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

func ParseFileEntry(value any) FileEntry { return ParseFileEntryWithWorkspace(value, "") }

func ParseFileEntryWithWorkspace(value any, workspaceCwd string) FileEntry {
	switch x := value.(type) {
	case string:
		return FileEntry{Path: pathdisplay.RenderWorkspaceDisplayPath(x, workspaceCwd)}
	case map[string]any:
		oldPath := strings.TrimSpace(firstNonEmpty(stringValue(x["oldPath"]), stringValue(x["old_path"])))
		newPath := strings.TrimSpace(firstNonEmpty(stringValue(x["newPath"]), stringValue(x["new_path"])))
		path := strings.TrimSpace(firstNonEmpty(stringValue(x["path"]), stringValue(x["file"]), stringValue(x["filePath"]), stringValue(x["file_path"]), stringValue(x["targetPath"]), stringValue(x["target_path"]), stringValue(x["name"])))
		if oldPath != "" && newPath != "" && oldPath != newPath {
			path = pathdisplay.RenderWorkspaceDisplayPath(oldPath, workspaceCwd) + " -> " + pathdisplay.RenderWorkspaceDisplayPath(newPath, workspaceCwd)
		} else {
			path = pathdisplay.RenderWorkspaceDisplayPath(path, workspaceCwd)
		}
		kind := strings.TrimSpace(firstNonEmpty(stringValue(x["kind"]), stringValue(x["changeType"]), stringValue(x["change_type"]), stringValue(x["op"]), stringValue(x["action"]), stringValue(x["status"]), stringValue(x["type"])))
		return FileEntry{Path: path, Kind: kind}
	default:
		return FileEntry{}
	}
}

func TruncatedRequestJSON(params map[string]any) string {
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
	return apputil.Truncate(prettyJSON(trimmed), 800)
}

func firstNonEmpty(values ...string) string {
	return apputil.FirstNonEmpty(values...)
}

func stringValue(v any) string { return apputil.StringValue(v) }

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

func prettyJSON(v any) string { return apputil.PrettyJSON(v) }

func markdownCodeBlock(s string) string { return apputil.MarkdownCodeBlock(s) }
