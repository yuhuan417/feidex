package app

import (
	"fmt"
	"strings"
)

type claudeDynamicToolCategory string

const (
	claudeDynamicToolUnknownCategory claudeDynamicToolCategory = ""
	claudeDynamicToolCodeCategory    claudeDynamicToolCategory = "code"
	claudeDynamicToolShellCategory   claudeDynamicToolCategory = "shell"
	claudeDynamicToolWebCategory     claudeDynamicToolCategory = "web"
	claudeDynamicToolTodoCategory    claudeDynamicToolCategory = "todo"
	claudeDynamicToolPlanCategory    claudeDynamicToolCategory = "plan"
	claudeDynamicToolTaskCategory    claudeDynamicToolCategory = "task"
	claudeDynamicToolSkillCategory   claudeDynamicToolCategory = "skill"
	claudeDynamicToolMCPCategory     claudeDynamicToolCategory = "mcp"
)

type claudeDynamicToolCardTemplate struct {
	Title   string
	Color   string
	Summary string
	Detail  string
}

func classifyClaudeDynamicTool(toolName string) claudeDynamicToolCategory {
	toolName = strings.TrimSpace(toolName)
	switch {
	case strings.HasPrefix(strings.ToLower(toolName), "mcp__"):
		return claudeDynamicToolMCPCategory
	}
	switch toolName {
	case "Read", "Edit", "Write", "NotebookEdit", "Glob", "Grep":
		return claudeDynamicToolCodeCategory
	case "Bash", "KillShell":
		return claudeDynamicToolShellCategory
	case "WebSearch", "WebFetch":
		return claudeDynamicToolWebCategory
	case "TodoWrite":
		return claudeDynamicToolTodoCategory
	case "EnterPlanMode", "AskUserQuestion", "ExitPlanMode":
		return claudeDynamicToolPlanCategory
	case "Task", "TaskOutput", "TaskCreate", "TaskGet", "TaskUpdate", "TaskList", "TaskStop", "SendMessage", "TeamCreate", "TeamDelete":
		return claudeDynamicToolTaskCategory
	case "Skill", "ToolSearch", "MCPSearch":
		return claudeDynamicToolSkillCategory
	default:
		return claudeDynamicToolUnknownCategory
	}
}

func buildClaudeDynamicToolCardTemplate(item map[string]any, workspaceCwd string) claudeDynamicToolCardTemplate {
	toolName := strings.TrimSpace(stringValue(item["tool"]))
	status := strings.TrimSpace(stringValue(item["status"]))
	input := item["input"]
	category := classifyClaudeDynamicTool(toolName)
	if category == claudeDynamicToolUnknownCategory {
		raw := strings.TrimSpace(prettyJSON(item))
		if raw == "" {
			return claudeDynamicToolCardTemplate{Title: "Claude 工具", Color: "grey"}
		}
		return claudeDynamicToolCardTemplate{
			Title:  "Claude 工具",
			Color:  "grey",
			Detail: markdownCodeBlockWithLang("json", raw),
		}
	}

	title, color := claudeDynamicToolCardMeta(category)
	bodyLines := buildClaudeDynamicToolSummaryLines(category, toolName, input, workspaceCwd)
	summaryLines := []string{title + ":"}
	summaryLines = append(summaryLines, bodyLines...)
	summaryLines = append(summaryLines, summarizeToolCallStatusLine(status))

	return claudeDynamicToolCardTemplate{
		Title:   title,
		Color:   color,
		Summary: strings.TrimSpace(strings.Join(trimmedNonEmptyStrings(summaryLines), "\n")),
		Detail:  summarizeToolCallDetail(claudeDisplayToolName(toolName), input, item),
	}
}

func claudeDynamicToolCardMeta(category claudeDynamicToolCategory) (string, string) {
	switch category {
	case claudeDynamicToolCodeCategory:
		return "代码工具", "orange"
	case claudeDynamicToolShellCategory:
		return "Shell 工具", "blue"
	case claudeDynamicToolWebCategory:
		return "网络工具", "blue"
	case claudeDynamicToolTodoCategory:
		return "待办更新", "green"
	case claudeDynamicToolPlanCategory:
		return "计划交互", "blue"
	case claudeDynamicToolTaskCategory:
		return "任务协作", "blue"
	case claudeDynamicToolSkillCategory:
		return "技能/工具发现", "grey"
	case claudeDynamicToolMCPCategory:
		return "MCP 工具", "blue"
	default:
		return "Claude 工具", "grey"
	}
}

func buildClaudeDynamicToolSummaryLines(category claudeDynamicToolCategory, toolName string, input any, workspaceCwd string) []string {
	switch category {
	case claudeDynamicToolCodeCategory:
		return buildClaudeCodeToolSummaryLines(toolName, input, workspaceCwd)
	case claudeDynamicToolShellCategory:
		return buildClaudeShellToolSummaryLines(toolName, input, workspaceCwd)
	case claudeDynamicToolWebCategory:
		return buildClaudeWebToolSummaryLines(toolName, input)
	case claudeDynamicToolTodoCategory:
		return buildClaudeTodoToolSummaryLines(toolName, input)
	case claudeDynamicToolPlanCategory:
		return buildClaudePlanToolSummaryLines(toolName, input)
	case claudeDynamicToolTaskCategory:
		return buildClaudeTaskToolSummaryLines(toolName, input)
	case claudeDynamicToolSkillCategory:
		return buildClaudeSkillToolSummaryLines(toolName, input)
	case claudeDynamicToolMCPCategory:
		return buildClaudeMCPToolSummaryLines(toolName, input, workspaceCwd)
	default:
		return nil
	}
}

func buildClaudeQuietDynamicToolLines(toolName string, input any, workspaceCwd string) []string {
	switch classifyClaudeDynamicTool(toolName) {
	case claudeDynamicToolCodeCategory:
		return buildClaudeCodeToolProgressLines(toolName, input, workspaceCwd)
	case claudeDynamicToolShellCategory:
		return buildClaudeShellToolProgressLines(toolName, input, workspaceCwd)
	case claudeDynamicToolWebCategory:
		return buildClaudeWebToolProgressLines(toolName, input)
	case claudeDynamicToolTodoCategory:
		return buildClaudeTodoToolProgressLines(input)
	case claudeDynamicToolPlanCategory:
		return buildClaudePlanToolProgressLines(toolName, input)
	case claudeDynamicToolTaskCategory:
		return buildClaudeTaskToolProgressLines(toolName, input)
	case claudeDynamicToolSkillCategory:
		return buildClaudeSkillToolProgressLines(toolName, input)
	case claudeDynamicToolMCPCategory:
		return buildClaudeMCPToolProgressLines(toolName, input, workspaceCwd)
	default:
		return nil
	}
}

func buildClaudeCodeToolSummaryLines(toolName string, input any, workspaceCwd string) []string {
	m := toolInputMap(input)
	lines := []string{claudeSummaryInlineLine("工具", toolName)}
	path := claudeDynamicToolPath(m, workspaceCwd)
	switch strings.TrimSpace(toolName) {
	case "Read":
		lines = append(lines, claudeSummaryInlineLine("读取", path))
	case "Edit":
		lines = append(lines, claudeSummaryInlineLine("编辑", path))
	case "Write":
		lines = append(lines, claudeSummaryInlineLine("写入", path))
	case "NotebookEdit":
		lines = append(lines, claudeSummaryInlineLine("笔记本", path))
	case "Glob":
		lines = append(lines, claudeSummaryInlineLine("模式", claudeInputString(m, "glob", "pattern")))
		lines = append(lines, claudeSummaryInlineLine("范围", path))
	case "Grep":
		lines = append(lines, claudeSummaryInlineLine("检索", claudeInputString(m, "pattern", "query")))
		lines = append(lines, claudeSummaryInlineLine("范围", path))
	}
	if before, after := claudeInputString(m, "old_string"), claudeInputString(m, "new_string"); before != "" || after != "" {
		lines = append(lines, "- 替换: "+markdownInlineCode(truncate(before, 48))+" -> "+markdownInlineCode(truncate(after, 48)))
	}
	lines = append(lines, claudeSummaryInlineLine("目录", claudeInputStringRenderedPath(m, workspaceCwd, "cwd", "workdir")))
	return trimmedNonEmptyStrings(lines)
}

func buildClaudeShellToolSummaryLines(toolName string, input any, workspaceCwd string) []string {
	m := toolInputMap(input)
	lines := []string{claudeSummaryInlineLine("工具", toolName)}
	if command := claudeInputString(m, "command", "cmd"); command != "" {
		lines = append(lines, "命令:\n"+markdownCodeBlockWithLang("bash", truncate(command, 280)))
	}
	lines = append(lines, claudeSummaryInlineLine("目录", claudeInputStringRenderedPath(m, workspaceCwd, "cwd", "workdir")))
	return trimmedNonEmptyStrings(lines)
}

func buildClaudeWebToolSummaryLines(toolName string, input any) []string {
	m := toolInputMap(input)
	lines := []string{claudeSummaryInlineLine("工具", toolName)}
	if query := claudeInputString(m, "query"); query != "" {
		lines = append(lines, "搜索:\n"+markdownCodeBlock(truncate(query, 280)))
	}
	lines = append(lines, claudeSummaryInlineLine("URL", claudeInputString(m, "url")))
	lines = append(lines, claudeSummaryInlineLine("说明", claudeInputString(m, "prompt", "description")))
	return trimmedNonEmptyStrings(lines)
}

func buildClaudeTodoToolSummaryLines(toolName string, input any) []string {
	lines := []string{claudeSummaryInlineLine("工具", toolName)}
	if todoLines := summarizeTodoInputLines(toolInputMap(input)); len(todoLines) > 0 {
		lines = append(lines, todoLines...)
	}
	return trimmedNonEmptyStrings(lines)
}

func buildClaudePlanToolSummaryLines(toolName string, input any) []string {
	m := toolInputMap(input)
	lines := []string{claudeSummaryInlineLine("工具", toolName)}
	lines = append(lines, claudeSummaryInlineLine("说明", claudeInputString(m, "prompt", "description", "question")))
	return trimmedNonEmptyStrings(lines)
}

func buildClaudeTaskToolSummaryLines(toolName string, input any) []string {
	m := toolInputMap(input)
	lines := []string{claudeSummaryInlineLine("工具", toolName)}
	lines = append(lines, claudeSummaryInlineLine("任务", claudeInputString(m, "taskId", "task_id", "subject")))
	lines = append(lines, claudeSummaryInlineLine("团队", claudeInputString(m, "team_name", "team")))
	lines = append(lines, claudeSummaryInlineLine("负责人", claudeInputString(m, "owner", "recipient")))
	lines = append(lines, claudeSummaryInlineLine("状态", claudeInputString(m, "status", "type")))
	lines = append(lines, claudeSummaryInlineLine("说明", claudeInputString(m, "activeForm", "description", "content", "prompt")))
	return trimmedNonEmptyStrings(lines)
}

func buildClaudeSkillToolSummaryLines(toolName string, input any) []string {
	m := toolInputMap(input)
	lines := []string{claudeSummaryInlineLine("工具", toolName)}
	lines = append(lines, claudeSummaryInlineLine("查询", claudeInputString(m, "query", "prompt", "description")))
	lines = append(lines, claudeSummaryInlineLine("名称", claudeInputString(m, "name", "skill")))
	return trimmedNonEmptyStrings(lines)
}

func buildClaudeMCPToolSummaryLines(toolName string, input any, workspaceCwd string) []string {
	lines := []string{claudeSummaryInlineLine("工具", claudeDisplayToolName(toolName))}
	lines = append(lines, summarizeToolInputLines(input, workspaceCwd)...)
	return trimmedNonEmptyStrings(lines)
}

func buildClaudeCodeToolProgressLines(toolName string, input any, workspaceCwd string) []string {
	m := toolInputMap(input)
	path := claudeDynamicToolPath(m, workspaceCwd)
	switch strings.TrimSpace(toolName) {
	case "Read":
		return trimmedNonEmptyStrings([]string{"Read " + markdownInlineCode(quietDisplayFileName(path))})
	case "Edit", "Write", "NotebookEdit":
		return trimmedNonEmptyStrings([]string{"Update " + markdownInlineCode(quietDisplayFileName(path))})
	case "Glob":
		return trimmedNonEmptyStrings([]string{buildQuietSearchLine(claudeInputString(m, "glob", "pattern"), path)})
	case "Grep":
		return trimmedNonEmptyStrings([]string{buildQuietSearchLine(claudeInputString(m, "pattern", "query"), path)})
	default:
		return nil
	}
}

func buildClaudeShellToolProgressLines(toolName string, input any, workspaceCwd string) []string {
	m := toolInputMap(input)
	switch strings.TrimSpace(toolName) {
	case "Bash":
		command := claudeInputString(m, "command", "cmd")
		if command == "" {
			return nil
		}
		lines := []string{"Run " + markdownInlineCode(truncate(command, 80))}
		if cwd := claudeInputStringRenderedPath(m, workspaceCwd, "cwd", "workdir"); cwd != "" {
			lines = append(lines, "In "+markdownInlineCode(cwd))
		}
		return trimmedNonEmptyStrings(lines)
	case "KillShell":
		return []string{"Stop shell session"}
	default:
		return nil
	}
}

func buildClaudeWebToolProgressLines(toolName string, input any) []string {
	m := toolInputMap(input)
	switch strings.TrimSpace(toolName) {
	case "WebSearch":
		query := claudeInputString(m, "query")
		if query == "" {
			return nil
		}
		return []string{"Searching the web: " + markdownInlineCode(truncate(query, 120))}
	case "WebFetch":
		url := claudeInputString(m, "url")
		if url == "" {
			return nil
		}
		return []string{"Fetch " + markdownInlineCode(truncate(url, 120))}
	default:
		return nil
	}
}

func buildClaudeTodoToolProgressLines(input any) []string {
	m := toolInputMap(input)
	if len(m) == 0 {
		return nil
	}
	lines := []string{}
	todoLines := summarizeTodoInputLines(m)
	if len(todoLines) == 0 {
		return nil
	}
	if count := len(toolInputSequence(m["todos"])); count > 0 {
		lines = append(lines, fmt.Sprintf("Update todo list (%d items)", count))
	}
	for _, line := range todoLines {
		line = strings.TrimPrefix(strings.TrimSpace(line), "- ")
		if line != "" && !strings.HasPrefix(line, "todos:") {
			lines = append(lines, "Todo "+line)
		}
	}
	return trimmedNonEmptyStrings(lines)
}

func buildClaudePlanToolProgressLines(toolName string, input any) []string {
	m := toolInputMap(input)
	switch strings.TrimSpace(toolName) {
	case "EnterPlanMode":
		if description := claudeInputString(m, "prompt", "description"); description != "" {
			return []string{"Enter plan mode: " + markdownInlineCode(truncate(description, 100))}
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

func buildClaudeTaskToolProgressLines(toolName string, input any) []string {
	m := toolInputMap(input)
	switch strings.TrimSpace(toolName) {
	case "Task":
		description := claudeInputString(m, "description", "prompt", "task")
		if description == "" {
			return []string{"Spawn subtask"}
		}
		return []string{"Spawn subtask: " + markdownInlineCode(truncate(description, 100))}
	case "TaskCreate":
		subject := claudeInputString(m, "subject", "title", "description")
		if subject == "" {
			return []string{"Create task"}
		}
		return []string{"Create task: " + markdownInlineCode(truncate(subject, 100))}
	case "TaskGet":
		taskID := claudeInputString(m, "taskId", "task_id")
		if taskID == "" {
			return []string{"Read task"}
		}
		return []string{"Read task " + markdownInlineCode(taskID)}
	case "TaskUpdate":
		taskID := claudeInputString(m, "taskId", "task_id")
		status := claudeInputString(m, "status")
		activeForm := claudeInputString(m, "activeForm")
		switch {
		case taskID != "" && status != "" && strings.TrimSpace(activeForm) != "":
			return []string{"Update task " + markdownInlineCode(taskID) + " -> " + markdownInlineCode(status), "Progress " + markdownInlineCode(truncate(activeForm, 100))}
		case taskID != "" && status != "":
			return []string{"Update task " + markdownInlineCode(taskID) + " -> " + markdownInlineCode(status)}
		case taskID != "" && strings.TrimSpace(activeForm) != "":
			return []string{"Update task " + markdownInlineCode(taskID), "Progress " + markdownInlineCode(truncate(activeForm, 100))}
		case taskID != "":
			return []string{"Update task " + markdownInlineCode(taskID)}
		default:
			return []string{"Update task"}
		}
	case "TaskList":
		return []string{"List tasks"}
	case "TaskStop":
		taskID := claudeInputString(m, "taskId", "task_id")
		if taskID == "" {
			return []string{"Stop task"}
		}
		return []string{"Stop task " + markdownInlineCode(taskID)}
	case "TaskOutput":
		taskID := claudeInputString(m, "taskId", "task_id")
		if taskID == "" {
			return []string{"Read task output"}
		}
		return []string{"Read task output " + markdownInlineCode(taskID)}
	case "SendMessage":
		recipient := claudeInputString(m, "recipient", "owner")
		if recipient == "" {
			return []string{"Send agent message"}
		}
		return []string{"Send message to " + markdownInlineCode(recipient)}
	case "TeamCreate":
		team := claudeInputString(m, "team_name", "team")
		if team == "" {
			return []string{"Create team"}
		}
		return []string{"Create team " + markdownInlineCode(team)}
	case "TeamDelete":
		team := claudeInputString(m, "team_name", "team")
		if team == "" {
			return []string{"Delete team"}
		}
		return []string{"Delete team " + markdownInlineCode(team)}
	default:
		return nil
	}
}

func buildClaudeSkillToolProgressLines(toolName string, input any) []string {
	m := toolInputMap(input)
	switch strings.TrimSpace(toolName) {
	case "Skill":
		name := claudeInputString(m, "name", "skill", "query")
		if name == "" {
			return []string{"Use skill"}
		}
		return []string{"Use skill " + markdownInlineCode(truncate(name, 80))}
	case "ToolSearch":
		query := claudeInputString(m, "query", "prompt")
		if query == "" {
			return []string{"Search tools"}
		}
		return []string{"Search tools: " + markdownInlineCode(truncate(query, 100))}
	case "MCPSearch":
		query := claudeInputString(m, "query", "prompt")
		if query == "" {
			return []string{"Search MCP tools"}
		}
		return []string{"Search MCP tools: " + markdownInlineCode(truncate(query, 100))}
	default:
		return nil
	}
}

func buildClaudeMCPToolProgressLines(toolName string, input any, workspaceCwd string) []string {
	m := toolInputMap(input)
	lines := []string{"Call MCP tool " + markdownInlineCode(claudeDisplayToolName(toolName))}
	if path := claudeDynamicToolPath(m, workspaceCwd); path != "" {
		lines = append(lines, "Use "+markdownInlineCode(path))
	}
	if query := claudeInputString(m, "query", "pattern", "prompt"); query != "" {
		lines = append(lines, "Search "+markdownInlineCode(truncate(query, 100)))
	}
	return trimmedNonEmptyStrings(lines)
}

func claudeInputString(m map[string]any, keys ...string) string {
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

func claudeInputStringRenderedPath(m map[string]any, workspaceCwd string, keys ...string) string {
	value := claudeInputString(m, keys...)
	if value == "" {
		return ""
	}
	return renderWorkspaceDisplayPath(value, workspaceCwd)
}

func claudeDynamicToolPath(m map[string]any, workspaceCwd string) string {
	return renderWorkspaceDisplayPath(claudeInputString(
		m,
		"file_path",
		"path",
		"notebook_path",
		"target_path",
		"targetPath",
	), workspaceCwd)
}

func claudeSummaryInlineLine(label, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "- " + label + ": " + markdownInlineCode(truncate(value, 120))
}

func claudeDisplayToolName(toolName string) string {
	toolName = strings.TrimSpace(toolName)
	parts := strings.SplitN(toolName, "__", 3)
	if len(parts) == 3 && strings.EqualFold(parts[0], "mcp") {
		return strings.Trim(parts[1]+"/"+parts[2], "/")
	}
	return toolName
}
