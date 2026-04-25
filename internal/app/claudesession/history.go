package claudesession

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	appruntime "feidex/internal/app/runtime"
	"feidex/internal/app/apputil"
)

// FindSessionFile locates a Claude session JSONL file by session ID.
func FindSessionFile(sessionID string) (string, *appruntime.ClaudeSessionListMeta, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", nil, nil
	}
	dirs, _, err := SessionProjectDirs("", true)
	if err != nil {
		return "", nil, err
	}
	for _, dir := range dirs {
		filePath := filepath.Join(dir, sessionID+".jsonl")
		meta, err := ReadSessionListMeta(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			continue
		}
		if strings.TrimSpace(meta.ID) != sessionID {
			continue
		}
		metaCopy := meta
		return filePath, &metaCopy, nil
	}
	return "", nil, nil
}

// ReadHistoryTurns parses a Claude session JSONL file into turn summaries.
func ReadHistoryTurns(filePath string, markLatestCurrent bool) ([]appruntime.ClaudeHistoryTurnSummary, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	turns := make([]appruntime.ClaudeHistoryTurnSummary, 0, 32)
	var current *appruntime.ClaudeHistoryTurnSummary

	flushCurrent := func() {
		if current == nil {
			return
		}
		if current.Status == "" {
			current.Status = "completed"
		}
		current.Ordinal = len(turns) + 1
		turns = append(turns, *current)
		current = nil
	}

	for scanner.Scan() {
		entry, ok := ParseSessionLine(scanner.Text())
		if !ok {
			continue
		}
		record, include := HistoryRecordFromEntry(entry)
		if !include {
			continue
		}
		promptID := strings.TrimSpace(record.PromptID)
		if promptID != "" && HistoryStartsTurn(entry) && (current == nil || current.TurnID != promptID) {
			flushCurrent()
			current = &appruntime.ClaudeHistoryTurnSummary{TurnID: promptID, Preview: record.Preview}
		}
		if current == nil {
			if promptID == "" {
				continue
			}
			current = &appruntime.ClaudeHistoryTurnSummary{TurnID: promptID, Preview: record.Preview}
		}
		if current.Preview == "" {
			current.Preview = record.Preview
		}
		if promptID != "" && current.TurnID == "" {
			current.TurnID = promptID
		}
		current.Records = append(current.Records, record)
		if record.EntryType == "max_turns_reached" {
			current.Status = "failed"
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flushCurrent()
	if markLatestCurrent && len(turns) > 0 {
		turns[len(turns)-1].IsCurrent = true
		turns[len(turns)-1].Status = "running"
	}
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}
	return turns, nil
}

// HistoryStartsTurn returns true if the entry marks the beginning of a new turn.
func HistoryStartsTurn(entry map[string]any) bool {
	if strings.TrimSpace(StringValue(entry["type"])) != "user" {
		return false
	}
	message, _ := entry["message"].(map[string]any)
	return HistoryHasPromptContent(message["content"])
}

// HistoryHasPromptContent returns true if the content contains a non-empty prompt.
func HistoryHasPromptContent(content any) bool {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value) != ""
	case []any:
		for _, blockValue := range value {
			block, _ := blockValue.(map[string]any)
			switch strings.TrimSpace(StringValue(block["type"])) {
			case "text", "image":
				return true
			}
		}
	}
	return false
}

// HistoryRecordFromEntry converts a raw JSONL entry into a structured record.
func HistoryRecordFromEntry(entry map[string]any) (appruntime.ClaudeHistoryRecord, bool) {
	if sidechain, _ := entry["isSidechain"].(bool); sidechain {
		return appruntime.ClaudeHistoryRecord{}, false
	}
	recordType := strings.TrimSpace(StringValue(entry["type"]))
	record := appruntime.ClaudeHistoryRecord{
		EntryID:    strings.TrimSpace(StringValue(entry["uuid"])),
		EntryType:  recordType,
		Timestamp:  strings.TrimSpace(StringValue(entry["timestamp"])),
		PromptID:   strings.TrimSpace(StringValue(entry["promptId"])),
		ParentUUID: strings.TrimSpace(StringValue(entry["parentUuid"])),
	}
	switch recordType {
	case "user":
		message, _ := entry["message"].(map[string]any)
		record.Details = HistoryRenderEntryContent(message["content"])
	case "assistant":
		message, _ := entry["message"].(map[string]any)
		record.MessageID = strings.TrimSpace(StringValue(message["id"]))
		record.StopReason = strings.TrimSpace(StringValue(message["stop_reason"]))
		record.Details = HistoryRenderEntryContent(message["content"])
	case "system":
		if subtype := strings.TrimSpace(StringValue(entry["subtype"])); subtype != "" {
			record.EntryType = "system:" + subtype
		}
		record.Details = HistoryRenderSystemDetails(entry)
	case "max_turns_reached":
		record.Details = []string{"Claude 达到最大 turn 限制"}
	default:
		return appruntime.ClaudeHistoryRecord{}, false
	}
	if len(record.Details) == 0 {
		record.Details = []string{record.EntryType}
	}
	record.Preview = historyInputPreview(record.Details)
	return record, true
}

// HistoryRenderEntryContent extracts displayable text lines from message content.
func HistoryRenderEntryContent(content any) []string {
	switch value := content.(type) {
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return nil
		}
		return []string{text}
	case []any:
		rendered := make([]string, 0, len(value))
		for _, blockValue := range value {
			block, _ := blockValue.(map[string]any)
			blockType := strings.TrimSpace(StringValue(block["type"]))
			switch blockType {
			case "text":
				text := strings.TrimSpace(StringValue(block["text"]))
				if text != "" {
					rendered = append(rendered, text)
				}
			case "tool_use":
				name := firstNonEmpty(strings.TrimSpace(StringValue(block["name"])), "(unknown tool)")
				input, _ := block["input"].(map[string]any)
				detail := firstNonEmpty(strings.TrimSpace(StringValue(input["command"])), strings.TrimSpace(StringValue(input["description"])), strings.TrimSpace(StringValue(input["prompt"])))
				if detail == "" {
					rendered = append(rendered, "[tool_use] "+name)
				} else {
					rendered = append(rendered, "[tool_use] "+name+": "+detail)
				}
			case "tool_result":
				text := strings.TrimSpace(MessageText(block["content"]))
				if text == "" {
					text = strings.TrimSpace(StringValue(block["content"]))
				}
				if text == "" {
					text = "(no output)"
				}
				rendered = append(rendered, "[tool_result] "+text)
			case "image":
				rendered = append(rendered, "[image]")
			default:
				if blockType != "" {
					rendered = append(rendered, "["+blockType+"]")
				}
			}
		}
		return rendered
	default:
		return nil
	}
}

// HistoryRenderSystemDetails extracts displayable details from a system entry.
func HistoryRenderSystemDetails(entry map[string]any) []string {
	details := []string{}
	if subtype := strings.TrimSpace(StringValue(entry["subtype"])); subtype != "" {
		details = append(details, "subtype: "+subtype)
	}
	if cwd := strings.TrimSpace(StringValue(entry["cwd"])); cwd != "" {
		details = append(details, "cwd: "+cwd)
	}
	if sessionID := strings.TrimSpace(StringValue(entry["session_id"])); sessionID != "" {
		details = append(details, "session_id: "+sessionID)
	}
	return details
}

func historyInputPreview(inputs []string) string {
	if len(inputs) == 0 {
		return ""
	}
	if len(inputs) == 1 {
		return apputil.Truncate(inputs[0], 72)
	}
	return apputil.Truncate(inputs[0], 56) + fmt.Sprintf(" 等 %d 条", len(inputs))
}
