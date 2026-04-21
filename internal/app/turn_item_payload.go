package app

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

func buildTurnItemCardPayloadWithWorkspace(itemID string, item map[string]any, workspaceCwd string) (turnItemCardPayload, bool) {
	if item == nil {
		return turnItemCardPayload{}, false
	}

	itemType := normalizeTurnItemType(stringValue(item["type"]))
	itemID = strings.TrimSpace(firstNonEmpty(itemID, stringValue(item["id"])))
	payload := turnItemCardPayload{
		ItemID:           itemID,
		ItemType:         itemType,
		ProtocolItemType: itemType,
	}

	switch itemType {
	case "user_message":
		return turnItemCardPayload{}, false
	case "plan":
		text := stringValue(item["text"])
		payload.SummaryText = buildLabeledTurnEventText("计划", text)
	case "reasoning":
		text := firstNonEmpty(extractTurnItemText(item, "summary", "summary_text"), stringValue(item["text"]))
		if strings.TrimSpace(text) == "" {
			return turnItemCardPayload{}, false
		}
		payload.SummaryText = buildLabeledTurnEventText("思考", text)
	case "agent_message":
		text := firstNonEmpty(extractTurnItemText(item, "content", "output_text"), stringValue(item["text"]))
		payload.SummaryText = strings.TrimSpace(text)
		payload.IsFinalAnswer = strings.TrimSpace(stringValue(item["phase"])) == "final_answer"
	case "entered_review_mode":
		payload.SummaryText = "已进入 review 模式。"
	case "exited_review_mode":
		payload.SummaryText = strings.TrimSpace(stringValue(item["review"]))
		if payload.SummaryText == "" {
			payload.SummaryText = "Review 已完成。"
		}
		// Internally render review results through the normal final-answer path
		// while keeping the original protocol item type for lifecycle decisions.
		payload.ItemType = "agent_message"
		payload.IsFinalAnswer = true
	case "command_execution":
		command := firstNonEmpty(stringValue(item["command"]), stringValue(item["commandLine"]))
		output := firstNonEmpty(
			stringValue(item["aggregated_output"]),
			stringValue(item["aggregatedOutput"]),
			stringValue(item["output"]),
			extractTurnItemText(item, "content", "output_text"),
		)
		status := strings.TrimSpace(firstNonEmpty(stringValue(item["status"]), stringValue(item["state"])))
		exitCode, hasExitCode := intValue(item["exit_code"])
		if !hasExitCode {
			exitCode, hasExitCode = intValue(item["exitCode"])
		}
		payload.SummaryText = summarizeCommandExecution(command, output, status, optionalIntPointer(exitCode, hasExitCode))
		payload.DetailText = formatTurnCommandOutput(output)
	case "file_change":
		payload.SummaryText, payload.DetailText = summarizeFileChangeItem(item, workspaceCwd)
	case "dynamic_tool_call":
		payload.ToolName = strings.TrimSpace(stringValue(item["tool"]))
		template := buildClaudeDynamicToolCardTemplate(item, workspaceCwd)
		payload.SummaryText = template.Summary
		payload.DetailText = template.Detail
		payload.Title = template.Title
		payload.Color = template.Color
	default:
		summary, detail := summarizeGenericTurnItem(itemType, item, workspaceCwd)
		if strings.TrimSpace(summary) == "" && strings.TrimSpace(detail) == "" {
			return turnItemCardPayload{}, false
		}
		payload.SummaryText = summary
		payload.DetailText = detail
	}

	if strings.TrimSpace(payload.SummaryText) == "" && strings.TrimSpace(payload.DetailText) == "" {
		return turnItemCardPayload{}, false
	}
	if strings.TrimSpace(payload.Title) == "" || strings.TrimSpace(payload.Color) == "" {
		payload.Title, payload.Color = turnItemCardMeta(payload.ItemType, payload.IsFinalAnswer)
	}
	return payload, true
}

func buildLabeledTurnEventText(label, text string) string {
	text = strings.TrimSpace(text)
	if label == "" {
		return text
	}
	if text == "" {
		return label
	}
	return label + ":\n" + text
}

func summarizeCommandExecution(command, output, status string, exitCode *int) string {
	lines := []string{}
	if strings.TrimSpace(command) != "" {
		lines = append(lines, markdownCodeBlock(strings.TrimSpace(command)))
	}
	meta := make([]string, 0, 2)
	if strings.TrimSpace(status) != "" {
		meta = append(meta, "status="+strings.TrimSpace(status))
	}
	if exitCode != nil {
		meta = append(meta, "exit_code="+strconv.Itoa(*exitCode))
	}
	if len(meta) > 0 {
		lines = append(lines, strings.Join(meta, " "))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func formatTurnCommandOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	return "输出:\n" + markdownCodeBlock(output)
}

func summarizeFileChangeItem(item map[string]any, workspaceCwd string) (string, string) {
	changes, _ := item["changes"].([]any)
	status := strings.TrimSpace(firstNonEmpty(stringValue(item["status"]), stringValue(item["state"])))
	summaryBlock := make([]string, 0, 2+len(changes))
	if len(changes) > 0 {
		summaryBlock = append(summaryBlock, fmt.Sprintf("changed=%d", len(changes)))
	}
	if status != "" {
		summaryBlock = append(summaryBlock, "status="+status)
	}
	for _, raw := range changes {
		change, _ := raw.(map[string]any)
		path := renderWorkspaceDisplayPath(stringValue(change["path"]), workspaceCwd)
		kind := strings.TrimSpace(stringValue(change["kind"]))
		entry := path
		if kind != "" {
			entry = fmt.Sprintf("%s (%s)", path, kind)
		}
		if strings.TrimSpace(entry) != "" {
			summaryBlock = append(summaryBlock, entry)
		}
	}
	summaryLines := []string{"文件改动:"}
	if len(summaryBlock) > 0 {
		summaryLines = append(summaryLines, markdownCodeBlock(strings.Join(summaryBlock, "\n")))
	}
	detailLines := []string{}
	for _, raw := range changes {
		change, _ := raw.(map[string]any)
		path := renderWorkspaceDisplayPath(stringValue(change["path"]), workspaceCwd)
		kind := strings.TrimSpace(stringValue(change["kind"]))
		diff := strings.TrimSpace(stringValue(change["diff"]))
		header := path
		if kind != "" {
			header = fmt.Sprintf("%s (%s)", path, kind)
		}
		if header != "" {
			detailLines = append(detailLines, "", markdownCodeBlock(header))
		}
		if diff != "" {
			detailLines = append(detailLines, markdownCodeBlockWithLang("diff", diff))
			continue
		}
		if changeDetail := strings.TrimSpace(prettyJSON(change)); changeDetail != "" {
			detailLines = append(detailLines, markdownCodeBlock(changeDetail))
		}
	}
	detail := strings.TrimSpace(strings.Join(detailLines, "\n"))
	if len(changes) == 0 {
		raw := strings.TrimSpace(prettyJSON(item))
		if raw != "" {
			detail = markdownCodeBlock(raw)
		}
	}
	return strings.TrimSpace(strings.Join(summaryLines, "\n")), detail
}

func summarizeGenericTurnItem(itemType string, item map[string]any, workspaceCwd string) (string, string) {
	title := turnItemLabel(itemType)
	summaryLines := []string{title + ":"}
	switch normalizeTurnItemType(itemType) {
	case "mcp_tool_call":
		server := strings.TrimSpace(stringValue(item["server"]))
		tool := strings.TrimSpace(stringValue(item["tool"]))
		status := strings.TrimSpace(stringValue(item["status"]))
		toolName := strings.Trim(strings.TrimSpace(server+"/"+tool), "/")
		summaryLines = append(summaryLines, summarizeToolCallSummaryLines(toolName, item["input"], workspaceCwd)...)
		summaryLines = append(summaryLines, summarizeToolCallStatusLine(status))
		detail := summarizeToolCallDetail(toolName, item["input"], item)
		return strings.TrimSpace(strings.Join(trimmedNonEmptyStrings(summaryLines), "\n")), detail
	case "dynamic_tool_call":
		tool := strings.TrimSpace(stringValue(item["tool"]))
		status := strings.TrimSpace(stringValue(item["status"]))
		summaryLines = append(summaryLines, summarizeToolCallSummaryLines(tool, item["input"], workspaceCwd)...)
		summaryLines = append(summaryLines, summarizeToolCallStatusLine(status))
		detail := summarizeToolCallDetail(tool, item["input"], item)
		return strings.TrimSpace(strings.Join(trimmedNonEmptyStrings(summaryLines), "\n")), detail
	case "web_search":
		query := strings.TrimSpace(firstNonEmpty(stringValue(item["query"]), prettyJSON(item["action"])))
		if query != "" {
			summaryLines = append(summaryLines, markdownCodeBlock(query))
		}
	case "collab_agent_tool_call":
		tool := strings.TrimSpace(stringValue(item["tool"]))
		status := strings.TrimSpace(stringValue(item["status"]))
		summaryLines = append(summaryLines, summarizeToolCallSummaryLines(tool, item["input"], workspaceCwd)...)
		summaryLines = append(summaryLines, summarizeToolCallStatusLine(status))
		detail := summarizeToolCallDetail(tool, item["input"], item)
		return strings.TrimSpace(strings.Join(trimmedNonEmptyStrings(summaryLines), "\n")), detail
	default:
		summary := strings.TrimSpace(firstNonEmpty(
			stringValue(item["text"]),
			stringValue(item["output"]),
			extractTurnItemText(item, "summary", ""),
			prettyJSON(item),
		))
		if summary != "" {
			summaryLines = append(summaryLines, summary)
		}
	}
	detail := strings.TrimSpace(firstNonEmpty(
		stringValue(item["text"]),
		stringValue(item["output"]),
		extractTurnItemText(item, "content", ""),
		extractTurnItemText(item, "summary", ""),
		prettyJSON(item),
	))
	if isCodeStyledTurnItem(itemType) && detail != "" {
		detail = turnItemLabel(itemType) + ":\n" + markdownCodeBlock(detail)
	}
	return strings.TrimSpace(strings.Join(summaryLines, "\n")), detail
}

func summarizeToolCallSummaryLines(toolName string, input any, workspaceCwd string) []string {
	lines := []string{}
	if toolName = strings.TrimSpace(toolName); toolName != "" {
		lines = append(lines, markdownCodeBlock(toolName))
	}
	lines = append(lines, summarizeToolInputLines(input, workspaceCwd)...)
	return trimmedNonEmptyStrings(lines)
}

func summarizeToolCallStatusLine(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return ""
	}
	return "status=" + status
}

func summarizeToolCallDetail(toolName string, input any, item map[string]any) string {
	sections := []string{}
	if toolName = strings.TrimSpace(toolName); toolName != "" {
		sections = append(sections, "工具:\n"+markdownCodeBlock(toolName))
	}
	if inputJSON := strings.TrimSpace(prettyJSON(input)); inputJSON != "" && inputJSON != "{}" {
		sections = append(sections, "输入:\n"+markdownCodeBlockWithLang("json", inputJSON))
	}
	if len(sections) == 0 {
		if raw := strings.TrimSpace(prettyJSON(item)); raw != "" {
			sections = append(sections, markdownCodeBlockWithLang("json", raw))
		}
	}
	return joinMarkdownSections(sections...)
}

func summarizeToolInputLines(input any, workspaceCwd string) []string {
	m := toolInputMap(input)
	if len(m) == 0 {
		return nil
	}
	if lines := summarizeTodoInputLines(m); len(lines) > 0 {
		return lines
	}

	lines := []string{}
	addField := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		lines = append(lines, "- "+label+": "+markdownInlineCode(truncate(value, 120)))
	}
	addField("path", renderWorkspaceDisplayPath(firstNonEmpty(
		stringValue(m["file_path"]),
		stringValue(m["path"]),
		stringValue(m["notebook_path"]),
		stringValue(m["target_path"]),
		stringValue(m["targetPath"]),
	), workspaceCwd))
	addField("cwd", renderWorkspaceDisplayPath(firstNonEmpty(stringValue(m["cwd"]), stringValue(m["workdir"])), workspaceCwd))
	addField("command", firstNonEmpty(stringValue(m["command"]), stringValue(m["cmd"])))
	addField("description", stringValue(m["description"]))
	addField("query", stringValue(m["query"]))
	addField("url", stringValue(m["url"]))
	addField("prompt", stringValue(m["prompt"]))
	addField("pattern", stringValue(m["pattern"]))
	addField("glob", stringValue(m["glob"]))
	addField("mode", stringValue(m["mode"]))
	addField("task", firstNonEmpty(stringValue(m["taskId"]), stringValue(m["task_id"])))
	addField("status", stringValue(m["tool_status"]))
	if limit, ok := intValue(m["limit"]); ok {
		addField("limit", strconv.Itoa(limit))
	}
	if offset, ok := intValue(m["offset"]); ok {
		addField("offset", strconv.Itoa(offset))
	}
	if len(lines) > 0 {
		return limitSummaryLines(lines, 5)
	}
	return summarizeToolInputFallbackLines(m, workspaceCwd)
}

func summarizeTodoInputLines(input map[string]any) []string {
	rawTodos, ok := input["todos"]
	if !ok {
		return nil
	}
	items := toolInputSequence(rawTodos)
	if len(items) == 0 {
		return nil
	}
	lines := []string{fmt.Sprintf("- todos: %d", len(items))}
	const maxTodos = 4
	for i, raw := range items {
		if i >= maxTodos {
			lines = append(lines, fmt.Sprintf("- 还有 %d 项待办", len(items)-maxTodos))
			break
		}
		todo, _ := raw.(map[string]any)
		content := strings.TrimSpace(firstNonEmpty(
			stringValue(todo["content"]),
			stringValue(todo["title"]),
			stringValue(todo["activeForm"]),
		))
		if content == "" {
			continue
		}
		status := strings.TrimSpace(firstNonEmpty(stringValue(todo["status"]), stringValue(todo["state"])))
		if status != "" {
			lines = append(lines, fmt.Sprintf("- [%s] %s", status, truncate(content, 80)))
		} else {
			lines = append(lines, "- "+truncate(content, 80))
		}
	}
	return trimmedNonEmptyStrings(lines)
}

func summarizeToolInputFallbackLines(input map[string]any, workspaceCwd string) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		key = strings.TrimSpace(key)
		if key == "" || key == "todos" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := []string{}
	for _, key := range keys {
		if len(lines) >= 4 {
			break
		}
		switch value := input[key].(type) {
		case string:
			text := strings.TrimSpace(value)
			if text == "" {
				continue
			}
			if looksLikePathKey(key) {
				text = renderWorkspaceDisplayPath(text, workspaceCwd)
			}
			lines = append(lines, "- "+key+": "+markdownInlineCode(truncate(text, 120)))
		case bool:
			lines = append(lines, fmt.Sprintf("- %s: `%t`", key, value))
		case float64:
			lines = append(lines, fmt.Sprintf("- %s: `%g`", key, value))
		case int, int32, int64:
			lines = append(lines, fmt.Sprintf("- %s: `%v`", key, value))
		}
	}
	if len(lines) < len(keys) && len(lines) > 0 {
		lines = append(lines, fmt.Sprintf("- 还有 %d 项参数", len(keys)-len(lines)))
	}
	return lines
}

func toolInputMap(input any) map[string]any {
	if input == nil {
		return nil
	}
	if m, ok := input.(map[string]any); ok {
		return m
	}
	rv := reflect.ValueOf(input)
	if !rv.IsValid() || rv.Kind() != reflect.Map {
		return nil
	}
	if rv.Type().Key().Kind() != reflect.String {
		return nil
	}
	out := map[string]any{}
	iter := rv.MapRange()
	for iter.Next() {
		out[iter.Key().String()] = iter.Value().Interface()
	}
	return out
}

func toolInputSequence(value any) []any {
	if value == nil {
		return nil
	}
	if items, ok := value.([]any); ok {
		return items
	}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return nil
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out = append(out, rv.Index(i).Interface())
		}
		return out
	default:
		return nil
	}
}

func limitSummaryLines(lines []string, max int) []string {
	lines = trimmedNonEmptyStrings(lines)
	if max <= 0 || len(lines) <= max {
		return lines
	}
	result := append([]string(nil), lines[:max]...)
	result = append(result, fmt.Sprintf("- 还有 %d 项参数", len(lines)-max))
	return result
}

func looksLikePathKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "path", "file", "file_path", "filepath", "cwd", "workdir", "target_path", "targetpath", "notebook_path":
		return true
	default:
		return strings.HasSuffix(key, "_path") || strings.HasSuffix(key, "path")
	}
}
