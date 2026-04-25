package turnitem

import (
	"fmt"
	"strings"
)

type DynamicToolCategory string

const (
	DynamicToolUnknownCategory DynamicToolCategory = ""
	DynamicToolCodeCategory    DynamicToolCategory = "code"
	DynamicToolShellCategory   DynamicToolCategory = "shell"
	DynamicToolWebCategory     DynamicToolCategory = "web"
	DynamicToolTodoCategory    DynamicToolCategory = "todo"
	DynamicToolPlanCategory    DynamicToolCategory = "plan"
	DynamicToolTaskCategory    DynamicToolCategory = "task"
	DynamicToolSkillCategory   DynamicToolCategory = "skill"
	DynamicToolMCPCategory     DynamicToolCategory = "mcp"
)

type DynamicToolCardTemplate struct {
	Title   string
	Color   string
	Summary string
	Detail  string
}

func ClassifyDynamicTool(toolName string) DynamicToolCategory {
	toolName = strings.TrimSpace(toolName)
	switch {
	case strings.HasPrefix(strings.ToLower(toolName), "mcp__"):
		return DynamicToolMCPCategory
	}
	switch toolName {
	case "Read", "Edit", "Write", "NotebookEdit", "Glob", "Grep":
		return DynamicToolCodeCategory
	case "Bash", "KillShell":
		return DynamicToolShellCategory
	case "WebSearch", "WebFetch":
		return DynamicToolWebCategory
	case "TodoWrite":
		return DynamicToolTodoCategory
	case "EnterPlanMode", "AskUserQuestion", "ExitPlanMode":
		return DynamicToolPlanCategory
	case "Task", "TaskOutput", "TaskCreate", "TaskGet", "TaskUpdate", "TaskList", "TaskStop", "SendMessage", "TeamCreate", "TeamDelete":
		return DynamicToolTaskCategory
	case "Skill", "ToolSearch", "MCPSearch":
		return DynamicToolSkillCategory
	default:
		return DynamicToolUnknownCategory
	}
}

func BuildClaudeDynamicToolCardTemplate(item map[string]any, workspaceCwd string) DynamicToolCardTemplate {
	toolName := strings.TrimSpace(StringValue(item["tool"]))
	status := strings.TrimSpace(StringValue(item["status"]))
	input := item["input"]
	category := ClassifyDynamicTool(toolName)
	if category == DynamicToolUnknownCategory {
		raw := strings.TrimSpace(PrettyJSON(item))
		if raw == "" {
			return DynamicToolCardTemplate{Title: "Claude 工具", Color: "grey"}
		}
		return DynamicToolCardTemplate{
			Title:  "Claude 工具",
			Color:  "grey",
			Detail: MarkdownCodeBlockWithLang("json", raw),
		}
	}

	title, color := dynamicToolCardMeta(category)
	bodyLines := buildDynamicToolSummaryLines(category, toolName, input, workspaceCwd)
	summaryLines := []string{title + ":"}
	summaryLines = append(summaryLines, bodyLines...)
	summaryLines = append(summaryLines, SummarizeToolCallStatusLine(status))

	return DynamicToolCardTemplate{
		Title:   title,
		Color:   color,
		Summary: strings.TrimSpace(strings.Join(TrimmedNonEmptyStrings(summaryLines), "\n")),
		Detail:  SummarizeToolCallDetail(DisplayToolName(toolName), input, item),
	}
}

func BuildClaudeQuietDynamicToolLines(toolName string, input any, workspaceCwd string) []string {
	switch ClassifyDynamicTool(toolName) {
	case DynamicToolCodeCategory:
		return buildCodeToolProgressLines(toolName, input, workspaceCwd)
	case DynamicToolShellCategory:
		return buildShellToolProgressLines(toolName, input, workspaceCwd)
	case DynamicToolWebCategory:
		return buildWebToolProgressLines(toolName, input)
	case DynamicToolPlanCategory:
		return buildPlanToolProgressLines(toolName, input)
	case DynamicToolTaskCategory:
		return buildTaskToolProgressLines(toolName, input)
	case DynamicToolSkillCategory:
		return buildSkillToolProgressLines(toolName, input)
	case DynamicToolMCPCategory:
		return buildMCPToolProgressLines(toolName, input, workspaceCwd)
	default:
		return nil
	}
}

func dynamicToolCardMeta(category DynamicToolCategory) (string, string) {
	switch category {
	case DynamicToolCodeCategory:
		return "代码工具", "orange"
	case DynamicToolShellCategory:
		return "Shell 工具", "blue"
	case DynamicToolWebCategory:
		return "网络工具", "blue"
	case DynamicToolTodoCategory:
		return "待办更新", "green"
	case DynamicToolPlanCategory:
		return "计划交互", "blue"
	case DynamicToolTaskCategory:
		return "任务协作", "blue"
	case DynamicToolSkillCategory:
		return "技能/工具发现", "grey"
	case DynamicToolMCPCategory:
		return "MCP 工具", "blue"
	default:
		return "Claude 工具", "grey"
	}
}

func buildDynamicToolSummaryLines(category DynamicToolCategory, toolName string, input any, workspaceCwd string) []string {
	switch category {
	case DynamicToolCodeCategory:
		return buildCodeToolSummaryLines(toolName, input, workspaceCwd)
	case DynamicToolShellCategory:
		return buildShellToolSummaryLines(toolName, input, workspaceCwd)
	case DynamicToolWebCategory:
		return buildWebToolSummaryLines(toolName, input)
	case DynamicToolTodoCategory:
		return buildTodoToolSummaryLines(toolName, input)
	case DynamicToolPlanCategory:
		return buildPlanToolSummaryLines(toolName, input)
	case DynamicToolTaskCategory:
		return buildTaskToolSummaryLines(toolName, input)
	case DynamicToolSkillCategory:
		return buildSkillToolSummaryLines(toolName, input)
	case DynamicToolMCPCategory:
		return buildMCPToolSummaryLines(toolName, input, workspaceCwd)
	default:
		return nil
	}
}

func inputString(m map[string]any, keys ...string) string {
	if len(m) == 0 {
		return ""
	}
	for _, key := range keys {
		switch value := m[key].(type) {
		case string:
			if text := strings.TrimSpace(value); text != "" {
				return text
			}
		case int, int32, int64, float64, bool:
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func inputStringRenderedPath(m map[string]any, workspaceCwd string, keys ...string) string {
	value := inputString(m, keys...)
	if value == "" {
		return ""
	}
	return RenderWorkspaceDisplayPath(value, workspaceCwd)
}

func dynamicToolPath(m map[string]any, workspaceCwd string) string {
	return RenderWorkspaceDisplayPath(inputString(
		m,
		"file_path",
		"path",
		"notebook_path",
		"target_path",
		"targetPath",
	), workspaceCwd)
}

func summaryInlineLine(label, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "- " + label + ": " + MarkdownInlineCode(Truncate(value, 120))
}

func DisplayToolName(toolName string) string {
	toolName = strings.TrimSpace(toolName)
	parts := strings.SplitN(toolName, "__", 3)
	if len(parts) == 3 && strings.EqualFold(parts[0], "mcp") {
		return strings.Trim(parts[1]+"/"+parts[2], "/")
	}
	return toolName
}

func QuietDisplayFileName(path string) string {
	base := strings.TrimSpace(path)
	if base == "" {
		return base
	}
	// Split line reference like "file.go:42"
	for i, c := range base {
		if c == ':' && i > 0 {
			base = base[:i]
			break
		}
	}
	// Get just the filename
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '/' {
			return base[i+1:]
		}
	}
	return base
}

func BuildQuietSearchLine(query, path string) string {
	query = strings.TrimSpace(query)
	path = strings.TrimSpace(path)
	switch {
	case query != "" && path != "":
		return "Search " + MarkdownInlineCode(query) + " in " + MarkdownInlineCode(path)
	case query != "":
		return "Search " + MarkdownInlineCode(query)
	case path != "":
		return "Search in " + MarkdownInlineCode(path)
	default:
		return ""
	}
}

func buildCodeToolSummaryLines(toolName string, input any, workspaceCwd string) []string {
	m := ToolInputMap(input)
	lines := []string{summaryInlineLine("工具", toolName)}
	path := dynamicToolPath(m, workspaceCwd)
	switch strings.TrimSpace(toolName) {
	case "Read":
		lines = append(lines, summaryInlineLine("读取", path))
	case "Edit":
		lines = append(lines, summaryInlineLine("编辑", path))
	case "Write":
		lines = append(lines, summaryInlineLine("写入", path))
	case "NotebookEdit":
		lines = append(lines, summaryInlineLine("笔记本", path))
	case "Glob":
		lines = append(lines, summaryInlineLine("模式", inputString(m, "glob", "pattern")))
		lines = append(lines, summaryInlineLine("范围", path))
	case "Grep":
		lines = append(lines, summaryInlineLine("检索", inputString(m, "pattern", "query")))
		lines = append(lines, summaryInlineLine("范围", path))
	}
	if before, after := inputString(m, "old_string"), inputString(m, "new_string"); before != "" || after != "" {
		lines = append(lines, "- 替换: "+MarkdownInlineCode(Truncate(before, 48))+" -> "+MarkdownInlineCode(Truncate(after, 48)))
	}
	lines = append(lines, summaryInlineLine("目录", inputStringRenderedPath(m, workspaceCwd, "cwd", "workdir")))
	return TrimmedNonEmptyStrings(lines)
}

func buildShellToolSummaryLines(toolName string, input any, workspaceCwd string) []string {
	m := ToolInputMap(input)
	lines := []string{summaryInlineLine("工具", toolName)}
	if command := inputString(m, "command", "cmd"); command != "" {
		lines = append(lines, "命令:\n"+MarkdownCodeBlockWithLang("bash", Truncate(command, 280)))
	}
	lines = append(lines, summaryInlineLine("目录", inputStringRenderedPath(m, workspaceCwd, "cwd", "workdir")))
	return TrimmedNonEmptyStrings(lines)
}

func buildWebToolSummaryLines(toolName string, input any) []string {
	m := ToolInputMap(input)
	lines := []string{summaryInlineLine("工具", toolName)}
	if query := inputString(m, "query"); query != "" {
		lines = append(lines, "搜索:\n"+MarkdownCodeBlock(Truncate(query, 280)))
	}
	lines = append(lines, summaryInlineLine("URL", inputString(m, "url")))
	lines = append(lines, summaryInlineLine("说明", inputString(m, "prompt", "description")))
	return TrimmedNonEmptyStrings(lines)
}

func buildTodoToolSummaryLines(toolName string, input any) []string {
	lines := []string{summaryInlineLine("工具", toolName)}
	if todoLines := SummarizeTodoInputLines(ToolInputMap(input)); len(todoLines) > 0 {
		lines = append(lines, todoLines...)
	}
	return TrimmedNonEmptyStrings(lines)
}

func buildPlanToolSummaryLines(toolName string, input any) []string {
	m := ToolInputMap(input)
	lines := []string{summaryInlineLine("工具", toolName)}
	lines = append(lines, summaryInlineLine("说明", inputString(m, "prompt", "description", "question")))
	return TrimmedNonEmptyStrings(lines)
}

func buildTaskToolSummaryLines(toolName string, input any) []string {
	m := ToolInputMap(input)
	lines := []string{summaryInlineLine("工具", toolName)}
	lines = append(lines, summaryInlineLine("任务", inputString(m, "taskId", "task_id", "subject")))
	lines = append(lines, summaryInlineLine("团队", inputString(m, "team_name", "team")))
	lines = append(lines, summaryInlineLine("负责人", inputString(m, "owner", "recipient")))
	lines = append(lines, summaryInlineLine("状态", inputString(m, "status", "type")))
	lines = append(lines, summaryInlineLine("说明", inputString(m, "activeForm", "description", "content", "prompt")))
	return TrimmedNonEmptyStrings(lines)
}

func buildSkillToolSummaryLines(toolName string, input any) []string {
	m := ToolInputMap(input)
	lines := []string{summaryInlineLine("工具", toolName)}
	lines = append(lines, summaryInlineLine("查询", inputString(m, "query", "prompt", "description")))
	lines = append(lines, summaryInlineLine("名称", inputString(m, "name", "skill")))
	return TrimmedNonEmptyStrings(lines)
}

func buildMCPToolSummaryLines(toolName string, input any, workspaceCwd string) []string {
	lines := []string{summaryInlineLine("工具", DisplayToolName(toolName))}
	lines = append(lines, SummarizeToolInputLines(input, workspaceCwd)...)
	return TrimmedNonEmptyStrings(lines)
}

func buildCodeToolProgressLines(toolName string, input any, workspaceCwd string) []string {
	m := ToolInputMap(input)
	path := dynamicToolPath(m, workspaceCwd)
	switch strings.TrimSpace(toolName) {
	case "Read":
		return TrimmedNonEmptyStrings([]string{"Read " + MarkdownInlineCode(QuietDisplayFileName(path))})
	case "Edit", "Write", "NotebookEdit":
		return TrimmedNonEmptyStrings([]string{"Update " + MarkdownInlineCode(QuietDisplayFileName(path))})
	case "Glob":
		return TrimmedNonEmptyStrings([]string{BuildQuietSearchLine(inputString(m, "glob", "pattern"), path)})
	case "Grep":
		return TrimmedNonEmptyStrings([]string{BuildQuietSearchLine(inputString(m, "pattern", "query"), path)})
	default:
		return nil
	}
}

func buildShellToolProgressLines(toolName string, input any, workspaceCwd string) []string {
	switch strings.TrimSpace(toolName) {
	case "Bash":
		return nil
	case "KillShell":
		return []string{"Stop shell session"}
	default:
		return nil
	}
}

func buildWebToolProgressLines(toolName string, input any) []string {
	m := ToolInputMap(input)
	switch strings.TrimSpace(toolName) {
	case "WebSearch":
		query := inputString(m, "query")
		if query == "" {
			return nil
		}
		return []string{"Searching the web: " + MarkdownInlineCode(Truncate(query, 120))}
	case "WebFetch":
		url := inputString(m, "url")
		if url == "" {
			return nil
		}
		return []string{"Fetch " + MarkdownInlineCode(Truncate(url, 120))}
	default:
		return nil
	}
}

func buildPlanToolProgressLines(toolName string, input any) []string {
	m := ToolInputMap(input)
	switch strings.TrimSpace(toolName) {
	case "EnterPlanMode":
		if description := inputString(m, "prompt", "description"); description != "" {
			return []string{"Enter plan mode: " + MarkdownInlineCode(Truncate(description, 100))}
		}
		return []string{"Enter plan mode"}
	case "AskUserQuestion":
		return []string{"Ask for user input"}
	case "ExitPlanMode":
		return []string{"Exit plan mode"}
	default:
		return nil
	}
}

func buildTaskToolProgressLines(toolName string, input any) []string {
	m := ToolInputMap(input)
	switch strings.TrimSpace(toolName) {
	case "Task":
		description := inputString(m, "description", "prompt", "task")
		if description == "" {
			return []string{"Spawn subtask"}
		}
		return []string{"Spawn subtask: " + MarkdownInlineCode(Truncate(description, 100))}
	case "TaskCreate":
		subject := inputString(m, "subject", "title", "description")
		if subject == "" {
			return []string{"Create task"}
		}
		return []string{"Create task: " + MarkdownInlineCode(Truncate(subject, 100))}
	case "TaskGet":
		taskID := inputString(m, "taskId", "task_id")
		if taskID == "" {
			return []string{"Read task"}
		}
		return []string{"Read task " + MarkdownInlineCode(taskID)}
	case "TaskUpdate":
		taskID := inputString(m, "taskId", "task_id")
		status := inputString(m, "status")
		activeForm := inputString(m, "activeForm")
		switch {
		case taskID != "" && status != "" && strings.TrimSpace(activeForm) != "":
			return []string{"Update task " + MarkdownInlineCode(taskID) + " -> " + MarkdownInlineCode(status), "Progress " + MarkdownInlineCode(Truncate(activeForm, 100))}
		case taskID != "" && status != "":
			return []string{"Update task " + MarkdownInlineCode(taskID) + " -> " + MarkdownInlineCode(status)}
		case taskID != "" && strings.TrimSpace(activeForm) != "":
			return []string{"Update task " + MarkdownInlineCode(taskID), "Progress " + MarkdownInlineCode(Truncate(activeForm, 100))}
		case taskID != "":
			return []string{"Update task " + MarkdownInlineCode(taskID)}
		default:
			return []string{"Update task"}
		}
	case "TaskList":
		return []string{"List tasks"}
	case "TaskStop":
		taskID := inputString(m, "taskId", "task_id")
		if taskID == "" {
			return []string{"Stop task"}
		}
		return []string{"Stop task " + MarkdownInlineCode(taskID)}
	case "TaskOutput":
		taskID := inputString(m, "taskId", "task_id")
		if taskID == "" {
			return []string{"Read task output"}
		}
		return []string{"Read task output " + MarkdownInlineCode(taskID)}
	case "SendMessage":
		recipient := inputString(m, "recipient", "owner")
		if recipient == "" {
			return []string{"Send agent message"}
		}
		return []string{"Send message to " + MarkdownInlineCode(recipient)}
	case "TeamCreate":
		team := inputString(m, "team_name", "team")
		if team == "" {
			return []string{"Create team"}
		}
		return []string{"Create team " + MarkdownInlineCode(team)}
	case "TeamDelete":
		team := inputString(m, "team_name", "team")
		if team == "" {
			return []string{"Delete team"}
		}
		return []string{"Delete team " + MarkdownInlineCode(team)}
	default:
		return nil
	}
}

func buildSkillToolProgressLines(toolName string, input any) []string {
	m := ToolInputMap(input)
	switch strings.TrimSpace(toolName) {
	case "Skill":
		name := inputString(m, "name", "skill", "query")
		if name == "" {
			return []string{"Use skill"}
		}
		return []string{"Use skill " + MarkdownInlineCode(Truncate(name, 80))}
	case "ToolSearch":
		query := inputString(m, "query", "prompt")
		if query == "" {
			return []string{"Search tools"}
		}
		return []string{"Search tools: " + MarkdownInlineCode(Truncate(query, 100))}
	case "MCPSearch":
		query := inputString(m, "query", "prompt")
		if query == "" {
			return []string{"Search MCP tools"}
		}
		return []string{"Search MCP tools: " + MarkdownInlineCode(Truncate(query, 100))}
	default:
		return nil
	}
}

func buildMCPToolProgressLines(toolName string, input any, workspaceCwd string) []string {
	m := ToolInputMap(input)
	lines := []string{"Call MCP tool " + MarkdownInlineCode(DisplayToolName(toolName))}
	if path := dynamicToolPath(m, workspaceCwd); path != "" {
		lines = append(lines, "Use "+MarkdownInlineCode(path))
	}
	if query := inputString(m, "query", "pattern", "prompt"); query != "" {
		lines = append(lines, "Search "+MarkdownInlineCode(Truncate(query, 100)))
	}
	return TrimmedNonEmptyStrings(lines)
}
