package app

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

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
	return markdownCodeBlockWithLang("", s)
}

func markdownCodeBlockWithLang(lang, s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	fenceLen := maxConsecutiveBackticks(s) + 1
	if fenceLen < markdownFencePreferredLen {
		fenceLen = markdownFencePreferredLen
	}
	open := strings.Repeat("`", fenceLen)
	lang = strings.TrimSpace(strings.Trim(strings.TrimSpace(lang), "`"))
	if lang != "" {
		open += strings.ToLower(lang)
	}
	return open + "\n" + s + "\n" + strings.Repeat("`", fenceLen)
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
