package app

import (
	"fmt"
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
	default:
		summary, detail := summarizeGenericTurnItem(itemType, item)
		if strings.TrimSpace(summary) == "" && strings.TrimSpace(detail) == "" {
			return turnItemCardPayload{}, false
		}
		payload.SummaryText = summary
		payload.DetailText = detail
	}

	if strings.TrimSpace(payload.SummaryText) == "" && strings.TrimSpace(payload.DetailText) == "" {
		return turnItemCardPayload{}, false
	}
	payload.Title, payload.Color = turnItemCardMeta(payload.ItemType, payload.IsFinalAnswer)
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

func summarizeGenericTurnItem(itemType string, item map[string]any) (string, string) {
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
