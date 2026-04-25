package turnitem

import "strings"

type CardPayload struct {
	ItemID           string `json:"item_id"`
	ItemType         string `json:"item_type"`
	ProtocolItemType string `json:"protocol_item_type"`
	ToolName         string `json:"tool_name"`
	Title            string `json:"title"`
	Color            string `json:"color"`
	SummaryText      string `json:"summary_text"`
	DetailText       string `json:"detail_text"`
	IsFinalAnswer    bool   `json:"is_final_answer"`
}

func IsReplyTurnItem(itemType string) bool {
	switch NormalizeTurnItemType(itemType) {
	case "agent_message", "exited_review_mode":
		return true
	default:
		return false
	}
}

func ReplyTurnItemCardBody(payload CardPayload) string {
	body := StripTurnItemCardHeading(payload.SummaryText, payload.Title, payload.ItemType)
	if body == "" {
		body = StripTurnItemCardHeading(payload.DetailText, payload.Title, payload.ItemType)
	}
	return body
}

func ReplyTurnItemCardTitle(payload CardPayload) string {
	if payload.IsFinalAnswer {
		return payload.Title
	}
	return ""
}

func CompactTurnItemCardContent(payload CardPayload) (string, string) {
	summary := StripTurnItemCardHeading(payload.SummaryText, payload.Title, payload.ItemType)
	detail := StripTurnItemCardHeading(payload.DetailText, payload.Title, payload.ItemType)

	switch NormalizeTurnItemType(payload.ItemType) {
	case "command_execution":
		body, meta := SplitCompactMetaLine(summary)
		return meta, JoinMarkdownSections(body, detail)
	case "mcp_tool_call", "dynamic_tool_call", "collab_agent_tool_call":
		body, meta := SplitCompactMetaLine(summary)
		if body == "" {
			body = detail
		}
		return meta, body
	default:
		if summary != "" {
			return "", summary
		}
		return "", detail
	}
}

func StripTurnItemCardHeading(text, title, itemType string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	parts := strings.SplitN(text, "\n", 2)
	if len(parts) != 2 {
		return text
	}
	first := strings.TrimSpace(parts[0])
	if !strings.HasSuffix(first, ":") {
		return text
	}
	base := strings.TrimSuffix(first, ":")
	labels := []string{strings.TrimSpace(title), TurnItemLabel(itemType)}
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if base == label || strings.HasPrefix(base, label+"（") {
			return strings.TrimSpace(parts[1])
		}
	}
	return text
}

func SplitCompactMetaLine(text string) (string, string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	lines := strings.Split(text, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" || strings.HasPrefix(last, "```") {
		return text, ""
	}
	if !strings.Contains(last, "status=") && !strings.Contains(last, "exit_code=") {
		return text, ""
	}
	meta := strings.Join(strings.Fields(last), " · ")
	if len(lines) == 1 {
		return "", meta
	}
	return strings.TrimSpace(strings.Join(lines[:len(lines)-1], "\n")), meta
}

func JoinMarkdownSections(parts ...string) string {
	sections := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		sections = append(sections, part)
	}
	return strings.Join(sections, "\n\n")
}

func TurnItemCardMeta(itemType string, isFinalAnswer bool) (string, string) {
	if isFinalAnswer {
		return "最终答复", "green"
	}
	switch NormalizeTurnItemType(itemType) {
	case "reasoning":
		return "思考", "grey"
	case "command_execution":
		return "命令执行", "blue"
	case "file_change":
		return "文件改动", "orange"
	case "agent_message":
		return "回复", "green"
	case "entered_review_mode":
		return "进入 Review", "blue"
	case "exited_review_mode":
		return "Review 结果", "green"
	case "context_compaction":
		return "上下文压缩", "blue"
	default:
		return TurnItemLabel(itemType), "blue"
	}
}

func TurnItemEventKind(itemType string) string {
	switch NormalizeTurnItemType(itemType) {
	case "plan":
		return "turn_plan"
	case "reasoning":
		return "turn_reasoning"
	case "agent_message":
		return "turn_output"
	case "exited_review_mode":
		return "turn_output"
	case "command_execution":
		return "turn_command_execution"
	case "file_change":
		return "turn_file_change"
	case "context_compaction":
		return "turn_item"
	default:
		return "turn_item"
	}
}
