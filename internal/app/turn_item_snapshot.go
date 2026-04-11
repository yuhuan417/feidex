package app

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func snapshotTurnItem(buf *turnItemBuffer, item map[string]any, partial bool) turnItemSnapshot {
	if buf == nil && item == nil {
		return turnItemSnapshot{}
	}

	itemType := ""
	if buf != nil {
		itemType = buf.ItemType
	}
	if itemType == "" {
		itemType = normalizeTurnItemType(stringValue(item["type"]))
	}

	switch itemType {
	case "user_message":
		return turnItemSnapshot{}
	case "plan":
		text := firstNonEmpty(stringValue(item["text"]), strings.TrimSpace(deltaText(buf)))
		return turnItemSnapshot{
			ItemID:    itemIDValue(buf, item),
			ItemType:  itemType,
			StoreText: strings.TrimSpace(text),
			SendText:  buildLabeledTurnEventText("计划", text, partial),
			LinkKind:  "turn_plan",
		}
	case "reasoning":
		text := firstNonEmpty(extractTurnItemText(item, "summary", "summary_text"), strings.TrimSpace(deltaText(buf)), stringValue(item["text"]))
		if strings.TrimSpace(text) == "" {
			return turnItemSnapshot{}
		}
		return turnItemSnapshot{
			ItemID:    itemIDValue(buf, item),
			ItemType:  itemType,
			StoreText: text,
			SendText:  buildLabeledTurnEventText("思考", text, partial),
			LinkKind:  "turn_reasoning",
		}
	case "agent_message":
		text := firstNonEmpty(extractTurnItemText(item, "content", "output_text"), strings.TrimSpace(deltaText(buf)), stringValue(item["text"]))
		phase := strings.TrimSpace(stringValue(item["phase"]))
		isFinal := phase == "final_answer"
		label := ""
		if partial {
			label = "回复（未完成）"
		}
		sendText := strings.TrimSpace(text)
		if label != "" {
			sendText = buildLabeledTurnEventText(label, text, false)
		}
		return turnItemSnapshot{
			ItemID:        itemIDValue(buf, item),
			ItemType:      itemType,
			StoreText:     strings.TrimSpace(text),
			SendText:      sendText,
			LinkKind:      "turn_output",
			IsOutput:      true,
			IsFinalAnswer: isFinal,
		}
	case "command_execution":
		command := firstNonEmpty(stringValue(item["command"]), stringValue(item["commandLine"]), commandValue(buf))
		output := firstNonEmpty(
			stringValue(item["aggregated_output"]),
			stringValue(item["aggregatedOutput"]),
			stringValue(item["output"]),
			extractTurnItemText(item, "content", "output_text"),
			strings.TrimSpace(deltaText(buf)),
		)
		status := strings.TrimSpace(firstNonEmpty(stringValue(item["status"]), stringValue(item["state"])))
		exitCode, hasExitCode := intValue(item["exit_code"])
		if !hasExitCode {
			exitCode, hasExitCode = intValue(item["exitCode"])
		}
		summary := summarizeCommandExecution(command, output, status, optionalIntPointer(exitCode, hasExitCode))
		detail := formatTurnCommandOutput(output)
		return turnItemSnapshot{
			ItemID:     itemIDValue(buf, item),
			ItemType:   itemType,
			StoreText:  strings.TrimSpace(firstNonEmpty(output, formatTurnCommandEvent(command, output, status, nil, partial))),
			SendText:   summary,
			DetailText: detail,
			LinkKind:   "turn_command_execution",
			Expandable: strings.TrimSpace(detail) != "",
		}
	case "file_change":
		summary, detail := summarizeFileChangeItem(item)
		return turnItemSnapshot{
			ItemID:     itemIDValue(buf, item),
			ItemType:   itemType,
			StoreText:  strings.TrimSpace(detail),
			SendText:   summary,
			DetailText: detail,
			LinkKind:   "turn_file_change",
			Expandable: strings.TrimSpace(detail) != "",
		}
	default:
		summary, detail := summarizeGenericTurnItem(itemType, item, buf)
		if strings.TrimSpace(summary) == "" && strings.TrimSpace(detail) == "" {
			return turnItemSnapshot{}
		}
		storeText := strings.TrimSpace(detail)
		if storeText == "" {
			storeText = strings.TrimSpace(summary)
		}
		return turnItemSnapshot{
			ItemID:     itemIDValue(buf, item),
			ItemType:   itemType,
			StoreText:  storeText,
			SendText:   summary,
			DetailText: detail,
			LinkKind:   "turn_item",
			Expandable: strings.TrimSpace(detail) != "",
		}
	}
}

func buildLabeledTurnEventText(label, text string, partial bool) string {
	text = strings.TrimSpace(text)
	if label == "" {
		return text
	}
	if partial && !strings.Contains(label, "未完成") {
		label += "（未完成）"
	}
	if text == "" {
		return label
	}
	return label + ":\n" + text
}

func formatTurnCommandEvent(command, output, status string, exitCode *int, partial bool) string {
	lines := []string{}
	title := "命令执行"
	if partial {
		title = "命令执行（未完成）"
	}
	lines = append(lines, title+":")
	if strings.TrimSpace(command) != "" {
		lines = append(lines, "命令:")
		lines = append(lines, markdownCodeBlock("$ "+strings.TrimSpace(command)))
	}
	output = strings.TrimSpace(output)
	if output != "" {
		lines = append(lines, "输出:")
		lines = append(lines, markdownCodeBlock(output))
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

func summarizeFileChangeItem(item map[string]any) (string, string) {
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
		path := strings.TrimSpace(stringValue(change["path"]))
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
		path := strings.TrimSpace(stringValue(change["path"]))
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
			detailLines = append(detailLines, "```diff\n"+diff+"\n```")
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

func summarizeGenericTurnItem(itemType string, item map[string]any, buf *turnItemBuffer) (string, string) {
	title := turnItemLabel(itemType)
	summaryLines := []string{title + ":"}
	switch normalizeTurnItemType(itemType) {
	case "mcp_tool_call":
		server := strings.TrimSpace(stringValue(item["server"]))
		tool := strings.TrimSpace(stringValue(item["tool"]))
		status := strings.TrimSpace(stringValue(item["status"]))
		if server != "" || tool != "" {
			summaryLines = append(summaryLines, markdownCodeBlock(strings.TrimSpace(server+"/"+tool)))
		}
		if status != "" {
			summaryLines = append(summaryLines, "status="+status)
		}
	case "dynamic_tool_call":
		tool := strings.TrimSpace(stringValue(item["tool"]))
		status := strings.TrimSpace(stringValue(item["status"]))
		if tool != "" {
			summaryLines = append(summaryLines, markdownCodeBlock(tool))
		}
		if status != "" {
			summaryLines = append(summaryLines, "status="+status)
		}
	case "web_search":
		query := strings.TrimSpace(firstNonEmpty(stringValue(item["query"]), prettyJSON(item["action"])))
		if query != "" {
			summaryLines = append(summaryLines, markdownCodeBlock(query))
		}
	case "collab_agent_tool_call":
		tool := strings.TrimSpace(stringValue(item["tool"]))
		status := strings.TrimSpace(stringValue(item["status"]))
		if tool != "" {
			summaryLines = append(summaryLines, markdownCodeBlock(tool))
		}
		if status != "" {
			summaryLines = append(summaryLines, "status="+status)
		}
	default:
		summary := strings.TrimSpace(firstNonEmpty(
			stringValue(item["text"]),
			stringValue(item["output"]),
			extractTurnItemText(item, "summary", ""),
			strings.TrimSpace(deltaText(buf)),
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
	if detail == "" {
		detail = strings.TrimSpace(deltaText(buf))
	}
	if isCodeStyledTurnItem(itemType) && detail != "" {
		detail = turnItemLabel(itemType) + ":\n" + markdownCodeBlock(detail)
	}
	return strings.TrimSpace(strings.Join(summaryLines, "\n")), detail
}

func prettyJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func turnItemLabel(itemType string) string {
	switch normalizeTurnItemType(itemType) {
	case "reasoning":
		return "思考"
	case "agent_message":
		return "回复"
	case "command_execution":
		return "命令执行"
	case "file_change":
		return "文件改动"
	default:
		if strings.TrimSpace(itemType) == "" {
			return "事件"
		}
		return fmt.Sprintf("事件[%s]", strings.TrimSpace(itemType))
	}
}

func turnItemKey(itemID, itemType string) string {
	if strings.TrimSpace(itemID) != "" {
		return strings.TrimSpace(itemID)
	}
	normalized := normalizeTurnItemType(itemType)
	if normalized == "" {
		return "item"
	}
	return normalized
}

func normalizeTurnItemType(itemType string) string {
	itemType = strings.TrimSpace(itemType)
	itemType = strings.ReplaceAll(itemType, "UserMessage", "user_message")
	itemType = strings.ReplaceAll(itemType, "AgentMessage", "agent_message")
	itemType = strings.ReplaceAll(itemType, "CommandExecution", "command_execution")
	itemType = strings.ReplaceAll(itemType, "FileChange", "file_change")
	itemType = strings.ReplaceAll(itemType, "userMessage", "user_message")
	itemType = strings.ReplaceAll(itemType, "agentMessage", "agent_message")
	itemType = strings.ReplaceAll(itemType, "commandExecution", "command_execution")
	itemType = strings.ReplaceAll(itemType, "fileChange", "file_change")
	itemType = strings.ReplaceAll(itemType, "-", "_")
	return strings.ToLower(itemType)
}

func extractTurnItemText(item map[string]any, arrayField, elementType string) string {
	if item == nil {
		return ""
	}
	if arr, ok := item[arrayField].([]any); ok {
		parts := make([]string, 0, len(arr))
		for _, elem := range arr {
			m, ok := elem.(map[string]any)
			if !ok {
				continue
			}
			if elementType != "" && strings.TrimSpace(stringValue(m["type"])) != elementType {
				continue
			}
			if text := strings.TrimSpace(stringValue(m["text"])); text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return strings.TrimSpace(stringValue(item["text"]))
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func markdownCodeBlock(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "```", "'''")
	return "```\n" + s + "\n```"
}

func inlineCodeText(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "`", "'")
}

func isCodeStyledTurnItem(itemType string) bool {
	switch normalizeTurnItemType(itemType) {
	case "command_execution", "mcp_tool_call", "dynamic_tool_call", "web_search", "collab_agent_tool_call":
		return true
	default:
		return false
	}
}

func intValue(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case jsonNumber:
		i, err := strconv.Atoi(string(x))
		return i, err == nil
	default:
		return 0, false
	}
}

type jsonNumber string

func optionalIntPointer(v int, ok bool) *int {
	if !ok {
		return nil
	}
	value := v
	return &value
}

func itemIDValue(buf *turnItemBuffer, item map[string]any) string {
	if buf != nil && strings.TrimSpace(buf.ItemID) != "" {
		return strings.TrimSpace(buf.ItemID)
	}
	return strings.TrimSpace(stringValue(item["id"]))
}

func deltaText(buf *turnItemBuffer) string {
	if buf == nil {
		return ""
	}
	return buf.Delta
}

func commandValue(buf *turnItemBuffer) string {
	if buf == nil {
		return ""
	}
	return buf.Command
}

func appendSeparatedText(current, next string) string {
	current = strings.TrimSpace(current)
	next = strings.TrimSpace(next)
	if current == "" {
		return next
	}
	if next == "" {
		return current
	}
	return current + "\n\n" + next
}
