package turnitem

import (
	"fmt"
	"strconv"
	"strings"

	"feidex/internal/app/apputil"
)

const markdownFencePreferredLen = 4

type JSONNumber string

func PrettyJSON(v any) string { return apputil.PrettyJSON(v) }

func StringValue(v any) string { return apputil.StringValue(v) }

func InlineCodeText(s string) string { return apputil.InlineCodeText(s) }

func FirstNonEmpty(vals ...string) string { return apputil.FirstNonEmpty(vals...) }

func Truncate(s string, n int) string { return apputil.Truncate(s, n) }

func MarkdownInlineCode(s string) string {
	s = InlineCodeText(s)
	if s == "" {
		return ""
	}
	return "`" + s + "`"
}

func TrimmedNonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func MaxConsecutiveBackticks(s string) int {
	maxRun := 0
	run := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			run++
			if run > maxRun {
				maxRun = run
			}
			continue
		}
		run = 0
	}
	return maxRun
}

func MarkdownCodeBlock(s string) string {
	return MarkdownCodeBlockWithLang("", s)
}

func MarkdownCodeBlockWithLang(lang, s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	fenceLen := MaxConsecutiveBackticks(s) + 1
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

func NormalizeTurnItemType(itemType string) string {
	itemType = strings.TrimSpace(itemType)
	itemType = strings.ReplaceAll(itemType, "HookPrompt", "hook_prompt")
	itemType = strings.ReplaceAll(itemType, "WebSearch", "web_search")
	itemType = strings.ReplaceAll(itemType, "ImageView", "image_view")
	itemType = strings.ReplaceAll(itemType, "ImageGeneration", "image_generation")
	itemType = strings.ReplaceAll(itemType, "EnteredReviewMode", "entered_review_mode")
	itemType = strings.ReplaceAll(itemType, "ExitedReviewMode", "exited_review_mode")
	itemType = strings.ReplaceAll(itemType, "ContextCompaction", "context_compaction")
	itemType = strings.ReplaceAll(itemType, "McpToolCall", "mcp_tool_call")
	itemType = strings.ReplaceAll(itemType, "DynamicToolCall", "dynamic_tool_call")
	itemType = strings.ReplaceAll(itemType, "CollabAgentToolCall", "collab_agent_tool_call")
	itemType = strings.ReplaceAll(itemType, "UserMessage", "user_message")
	itemType = strings.ReplaceAll(itemType, "AgentMessage", "agent_message")
	itemType = strings.ReplaceAll(itemType, "CommandExecution", "command_execution")
	itemType = strings.ReplaceAll(itemType, "FileChange", "file_change")
	itemType = strings.ReplaceAll(itemType, "hookPrompt", "hook_prompt")
	itemType = strings.ReplaceAll(itemType, "webSearch", "web_search")
	itemType = strings.ReplaceAll(itemType, "imageView", "image_view")
	itemType = strings.ReplaceAll(itemType, "imageGeneration", "image_generation")
	itemType = strings.ReplaceAll(itemType, "enteredReviewMode", "entered_review_mode")
	itemType = strings.ReplaceAll(itemType, "exitedReviewMode", "exited_review_mode")
	itemType = strings.ReplaceAll(itemType, "contextCompaction", "context_compaction")
	itemType = strings.ReplaceAll(itemType, "mcpToolCall", "mcp_tool_call")
	itemType = strings.ReplaceAll(itemType, "dynamicToolCall", "dynamic_tool_call")
	itemType = strings.ReplaceAll(itemType, "collabAgentToolCall", "collab_agent_tool_call")
	itemType = strings.ReplaceAll(itemType, "userMessage", "user_message")
	itemType = strings.ReplaceAll(itemType, "agentMessage", "agent_message")
	itemType = strings.ReplaceAll(itemType, "commandExecution", "command_execution")
	itemType = strings.ReplaceAll(itemType, "fileChange", "file_change")
	itemType = strings.ReplaceAll(itemType, "-", "_")
	return strings.ToLower(itemType)
}

func TurnItemLabel(itemType string) string {
	switch NormalizeTurnItemType(itemType) {
	case "reasoning":
		return "思考"
	case "agent_message":
		return "回复"
	case "entered_review_mode":
		return "进入 Review"
	case "exited_review_mode":
		return "Review 结果"
	case "command_execution":
		return "命令执行"
	case "file_change":
		return "文件改动"
	case "context_compaction":
		return "上下文压缩"
	default:
		if strings.TrimSpace(itemType) == "" {
			return "事件"
		}
		return fmt.Sprintf("事件[%s]", strings.TrimSpace(itemType))
	}
}

func ExtractTurnItemText(item map[string]any, arrayField, elementType string) string {
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
			if elementType != "" && strings.TrimSpace(StringValue(m["type"])) != elementType {
				continue
			}
			if text := strings.TrimSpace(StringValue(m["text"])); text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return strings.TrimSpace(StringValue(item["text"]))
}

func IsCodeStyledTurnItem(itemType string) bool {
	switch NormalizeTurnItemType(itemType) {
	case "command_execution", "mcp_tool_call", "dynamic_tool_call", "web_search", "collab_agent_tool_call":
		return true
	default:
		return false
	}
}

func IntValue(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case JSONNumber:
		i, err := strconv.Atoi(string(x))
		return i, err == nil
	default:
		return 0, false
	}
}

func OptionalIntPointer(v int, ok bool) *int {
	if !ok {
		return nil
	}
	value := v
	return &value
}
