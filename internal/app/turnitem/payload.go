package turnitem

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"feidex/internal/pathdisplay"
)

func RenderWorkspaceDisplayPath(path, workspaceCwd string) string {
	return pathdisplay.RenderWorkspaceDisplayPath(path, workspaceCwd)
}

func BuildTurnItemCardPayload(itemID string, item map[string]any, workspaceCwd string) (CardPayload, bool) {
	if item == nil {
		return CardPayload{}, false
	}

	itemType := NormalizeTurnItemType(StringValue(item["type"]))
	itemID = strings.TrimSpace(FirstNonEmpty(itemID, StringValue(item["id"])))
	payload := CardPayload{
		ItemID:           itemID,
		ItemType:         itemType,
		ProtocolItemType: itemType,
	}

	switch itemType {
	case "user_message":
		return CardPayload{}, false
	case "plan":
		text := StringValue(item["text"])
		payload.SummaryText = BuildLabeledTurnEventText("计划", text)
	case "reasoning":
		text := FirstNonEmpty(ExtractTurnItemText(item, "summary", "summary_text"), StringValue(item["text"]))
		if strings.TrimSpace(text) == "" {
			return CardPayload{}, false
		}
		payload.SummaryText = BuildLabeledTurnEventText("思考", text)
	case "agent_message":
		text := FirstNonEmpty(ExtractTurnItemText(item, "content", "output_text"), StringValue(item["text"]))
		payload.SummaryText = strings.TrimSpace(text)
		payload.IsFinalAnswer = strings.TrimSpace(StringValue(item["phase"])) == "final_answer"
	case "entered_review_mode":
		payload.SummaryText = "已进入 review 模式。"
	case "exited_review_mode":
		payload.SummaryText = strings.TrimSpace(StringValue(item["review"]))
		if payload.SummaryText == "" {
			payload.SummaryText = "Review 已完成。"
		}
		payload.ItemType = "agent_message"
		payload.IsFinalAnswer = true
	case "command_execution":
		command := FirstNonEmpty(StringValue(item["command"]), StringValue(item["commandLine"]))
		output := FirstNonEmpty(
			StringValue(item["aggregated_output"]),
			StringValue(item["aggregatedOutput"]),
			StringValue(item["output"]),
			ExtractTurnItemText(item, "content", "output_text"),
		)
		status := strings.TrimSpace(FirstNonEmpty(StringValue(item["status"]), StringValue(item["state"])))
		exitCode, hasExitCode := IntValue(item["exit_code"])
		if !hasExitCode {
			exitCode, hasExitCode = IntValue(item["exitCode"])
		}
		payload.SummaryText = SummarizeCommandExecution(command, output, status, OptionalIntPointer(exitCode, hasExitCode))
		payload.DetailText = FormatTurnCommandOutput(output)
	case "file_change":
		payload.SummaryText, payload.DetailText = SummarizeFileChangeItem(item, workspaceCwd)
	case "dynamic_tool_call":
		payload.ToolName = strings.TrimSpace(StringValue(item["tool"]))
		template := BuildClaudeDynamicToolCardTemplate(item, workspaceCwd)
		payload.SummaryText = template.Summary
		payload.DetailText = template.Detail
		payload.Title = template.Title
		payload.Color = template.Color
	default:
		summary, detail := SummarizeGenericTurnItem(itemType, item, workspaceCwd)
		if strings.TrimSpace(summary) == "" && strings.TrimSpace(detail) == "" {
			return CardPayload{}, false
		}
		payload.SummaryText = summary
		payload.DetailText = detail
	}

	if strings.TrimSpace(payload.SummaryText) == "" && strings.TrimSpace(payload.DetailText) == "" {
		return CardPayload{}, false
	}
	if strings.TrimSpace(payload.Title) == "" || strings.TrimSpace(payload.Color) == "" {
		payload.Title, payload.Color = TurnItemCardMeta(payload.ItemType, payload.IsFinalAnswer)
	}
	return payload, true
}

func BuildLabeledTurnEventText(label, text string) string {
	text = strings.TrimSpace(text)
	if label == "" {
		return text
	}
	if text == "" {
		return label
	}
	return label + ":\n" + text
}

func SummarizeCommandExecution(command, output, status string, exitCode *int) string {
	lines := []string{}
	if strings.TrimSpace(command) != "" {
		lines = append(lines, MarkdownCodeBlock(strings.TrimSpace(command)))
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

func FormatTurnCommandOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	return "输出:\n" + MarkdownCodeBlock(output)
}

func SummarizeFileChangeItem(item map[string]any, workspaceCwd string) (string, string) {
	changes, _ := item["changes"].([]any)
	status := strings.TrimSpace(FirstNonEmpty(StringValue(item["status"]), StringValue(item["state"])))
	summaryBlock := make([]string, 0, 2+len(changes))
	if len(changes) > 0 {
		summaryBlock = append(summaryBlock, fmt.Sprintf("changed=%d", len(changes)))
	}
	if status != "" {
		summaryBlock = append(summaryBlock, "status="+status)
	}
	for _, raw := range changes {
		change, _ := raw.(map[string]any)
		path := RenderWorkspaceDisplayPath(StringValue(change["path"]), workspaceCwd)
		kind := strings.TrimSpace(StringValue(change["kind"]))
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
		summaryLines = append(summaryLines, MarkdownCodeBlock(strings.Join(summaryBlock, "\n")))
	}
	detailLines := []string{}
	for _, raw := range changes {
		change, _ := raw.(map[string]any)
		path := RenderWorkspaceDisplayPath(StringValue(change["path"]), workspaceCwd)
		kind := strings.TrimSpace(StringValue(change["kind"]))
		diff := strings.TrimSpace(StringValue(change["diff"]))
		header := path
		if kind != "" {
			header = fmt.Sprintf("%s (%s)", path, kind)
		}
		if header != "" {
			detailLines = append(detailLines, "", MarkdownCodeBlock(header))
		}
		if diff != "" {
			detailLines = append(detailLines, MarkdownCodeBlockWithLang("diff", diff))
			continue
		}
		if changeDetail := strings.TrimSpace(PrettyJSON(change)); changeDetail != "" {
			detailLines = append(detailLines, MarkdownCodeBlock(changeDetail))
		}
	}
	detail := strings.TrimSpace(strings.Join(detailLines, "\n"))
	if len(changes) == 0 {
		raw := strings.TrimSpace(PrettyJSON(item))
		if raw != "" {
			detail = MarkdownCodeBlock(raw)
		}
	}
	return strings.TrimSpace(strings.Join(summaryLines, "\n")), detail
}

func SummarizeGenericTurnItem(itemType string, item map[string]any, workspaceCwd string) (string, string) {
	title := TurnItemLabel(itemType)
	summaryLines := []string{title + ":"}
	switch NormalizeTurnItemType(itemType) {
	case "mcp_tool_call":
		server := strings.TrimSpace(StringValue(item["server"]))
		tool := strings.TrimSpace(StringValue(item["tool"]))
		status := strings.TrimSpace(StringValue(item["status"]))
		toolName := strings.Trim(strings.TrimSpace(server+"/"+tool), "/")
		summaryLines = append(summaryLines, SummarizeToolCallSummaryLines(toolName, item["input"], workspaceCwd)...)
		summaryLines = append(summaryLines, SummarizeToolCallStatusLine(status))
		detail := SummarizeToolCallDetail(toolName, item["input"], item)
		return strings.TrimSpace(strings.Join(TrimmedNonEmptyStrings(summaryLines), "\n")), detail
	case "dynamic_tool_call":
		tool := strings.TrimSpace(StringValue(item["tool"]))
		status := strings.TrimSpace(StringValue(item["status"]))
		summaryLines = append(summaryLines, SummarizeToolCallSummaryLines(tool, item["input"], workspaceCwd)...)
		summaryLines = append(summaryLines, SummarizeToolCallStatusLine(status))
		detail := SummarizeToolCallDetail(tool, item["input"], item)
		return strings.TrimSpace(strings.Join(TrimmedNonEmptyStrings(summaryLines), "\n")), detail
	case "web_search":
		query := strings.TrimSpace(FirstNonEmpty(StringValue(item["query"]), PrettyJSON(item["action"])))
		if query != "" {
			summaryLines = append(summaryLines, MarkdownCodeBlock(query))
		}
	case "collab_agent_tool_call":
		tool := strings.TrimSpace(StringValue(item["tool"]))
		status := strings.TrimSpace(StringValue(item["status"]))
		summaryLines = append(summaryLines, SummarizeToolCallSummaryLines(tool, item["input"], workspaceCwd)...)
		summaryLines = append(summaryLines, SummarizeToolCallStatusLine(status))
		detail := SummarizeToolCallDetail(tool, item["input"], item)
		return strings.TrimSpace(strings.Join(TrimmedNonEmptyStrings(summaryLines), "\n")), detail
	default:
		summary := strings.TrimSpace(FirstNonEmpty(
			StringValue(item["text"]),
			StringValue(item["output"]),
			ExtractTurnItemText(item, "summary", ""),
			PrettyJSON(item),
		))
		if summary != "" {
			summaryLines = append(summaryLines, summary)
		}
	}
	detail := strings.TrimSpace(FirstNonEmpty(
		StringValue(item["text"]),
		StringValue(item["output"]),
		ExtractTurnItemText(item, "content", ""),
		ExtractTurnItemText(item, "summary", ""),
		PrettyJSON(item),
	))
	if IsCodeStyledTurnItem(itemType) && detail != "" {
		detail = TurnItemLabel(itemType) + ":\n" + MarkdownCodeBlock(detail)
	}
	return strings.TrimSpace(strings.Join(summaryLines, "\n")), detail
}

func SummarizeToolCallSummaryLines(toolName string, input any, workspaceCwd string) []string {
	lines := []string{}
	if toolName = strings.TrimSpace(toolName); toolName != "" {
		lines = append(lines, MarkdownCodeBlock(toolName))
	}
	lines = append(lines, SummarizeToolInputLines(input, workspaceCwd)...)
	return TrimmedNonEmptyStrings(lines)
}

func SummarizeToolCallStatusLine(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return ""
	}
	return "status=" + status
}

func SummarizeToolCallDetail(toolName string, input any, item map[string]any) string {
	sections := []string{}
	if toolName = strings.TrimSpace(toolName); toolName != "" {
		sections = append(sections, "工具:\n"+MarkdownCodeBlock(toolName))
	}
	if inputJSON := strings.TrimSpace(PrettyJSON(input)); inputJSON != "" && inputJSON != "{}" {
		sections = append(sections, "输入:\n"+MarkdownCodeBlockWithLang("json", inputJSON))
	}
	if len(sections) == 0 {
		if raw := strings.TrimSpace(PrettyJSON(item)); raw != "" {
			sections = append(sections, MarkdownCodeBlockWithLang("json", raw))
		}
	}
	return JoinMarkdownSections(sections...)
}

func SummarizeToolInputLines(input any, workspaceCwd string) []string {
	m := ToolInputMap(input)
	if len(m) == 0 {
		return nil
	}
	if lines := SummarizeTodoInputLines(m); len(lines) > 0 {
		return lines
	}

	lines := []string{}
	addField := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		lines = append(lines, "- "+label+": "+MarkdownInlineCode(Truncate(value, 120)))
	}
	addField("path", RenderWorkspaceDisplayPath(FirstNonEmpty(
		StringValue(m["file_path"]),
		StringValue(m["path"]),
		StringValue(m["notebook_path"]),
		StringValue(m["target_path"]),
		StringValue(m["targetPath"]),
	), workspaceCwd))
	addField("cwd", RenderWorkspaceDisplayPath(FirstNonEmpty(StringValue(m["cwd"]), StringValue(m["workdir"])), workspaceCwd))
	addField("command", FirstNonEmpty(StringValue(m["command"]), StringValue(m["cmd"])))
	addField("description", StringValue(m["description"]))
	addField("query", StringValue(m["query"]))
	addField("url", StringValue(m["url"]))
	addField("prompt", StringValue(m["prompt"]))
	addField("pattern", StringValue(m["pattern"]))
	addField("glob", StringValue(m["glob"]))
	addField("mode", StringValue(m["mode"]))
	addField("task", FirstNonEmpty(StringValue(m["taskId"]), StringValue(m["task_id"])))
	addField("status", StringValue(m["tool_status"]))
	if limit, ok := IntValue(m["limit"]); ok {
		addField("limit", strconv.Itoa(limit))
	}
	if offset, ok := IntValue(m["offset"]); ok {
		addField("offset", strconv.Itoa(offset))
	}
	if len(lines) > 0 {
		return LimitSummaryLines(lines, 5)
	}
	return SummarizeToolInputFallbackLines(m, workspaceCwd)
}

func SummarizeTodoInputLines(input map[string]any) []string {
	rawTodos, ok := input["todos"]
	if !ok {
		return nil
	}
	items := ToolInputSequence(rawTodos)
	if len(items) == 0 {
		return nil
	}
	lines := []string{fmt.Sprintf("- todos: %d", len(items))}
	for _, raw := range items {
		todo, _ := raw.(map[string]any)
		content := strings.TrimSpace(FirstNonEmpty(
			StringValue(todo["content"]),
			StringValue(todo["title"]),
			StringValue(todo["activeForm"]),
		))
		if content == "" {
			continue
		}
		status := strings.TrimSpace(FirstNonEmpty(StringValue(todo["status"]), StringValue(todo["state"])))
		if status != "" {
			lines = append(lines, fmt.Sprintf("- [%s] %s", status, Truncate(content, 80)))
		} else {
			lines = append(lines, "- "+Truncate(content, 80))
		}
	}
	return TrimmedNonEmptyStrings(lines)
}

func SummarizeToolInputFallbackLines(input map[string]any, workspaceCwd string) []string {
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
			if LooksLikePathKey(key) {
				text = RenderWorkspaceDisplayPath(text, workspaceCwd)
			}
			lines = append(lines, "- "+key+": "+MarkdownInlineCode(Truncate(text, 120)))
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

func ToolInputMap(input any) map[string]any {
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

func ToolInputSequence(value any) []any {
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

func LimitSummaryLines(lines []string, max int) []string {
	lines = TrimmedNonEmptyStrings(lines)
	if max <= 0 || len(lines) <= max {
		return lines
	}
	result := append([]string(nil), lines[:max]...)
	result = append(result, fmt.Sprintf("- 还有 %d 项参数", len(lines)-max))
	return result
}

func LooksLikePathKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "path", "file", "file_path", "filepath", "cwd", "workdir", "target_path", "targetpath", "notebook_path":
		return true
	default:
		return strings.HasSuffix(key, "_path") || strings.HasSuffix(key, "path")
	}
}
