package app

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type claudeHistoryRecord struct {
	EntryID    string
	EntryType  string
	Timestamp  string
	PromptID   string
	MessageID  string
	ParentUUID string
	StopReason string
	Preview    string
	Details    []string
}

type claudeHistoryTurnSummary struct {
	Ordinal   int
	TurnID    string
	Status    string
	Preview   string
	Records   []claudeHistoryRecord
	IsCurrent bool
}

func (a *App) historyTurnIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	_, _, turns, err := a.fetchClaudeCurrentSessionTurns(sessionKey)
	if err != nil {
		return 0, err
	}
	for idx, turn := range turns {
		if turn.Ordinal == ordinal {
			return idx, nil
		}
	}
	return 0, fmt.Errorf("Turn #%d 不存在", ordinal)
}

func (a *App) renderClaudeHistoryCard(sessionKey string, page int) (map[string]any, error) {
	sess, thread, turns, err := a.fetchClaudeCurrentSessionTurns(sessionKey)
	if err != nil {
		return nil, err
	}
	if page < 0 {
		page = 0
	}
	total := len(turns)
	start := page * historyPageSize
	if start >= total && total > 0 {
		page = (total - 1) / historyPageSize
		start = page * historyPageSize
	}
	end := start + historyPageSize
	if end > total {
		end = total
	}
	label := currentThreadLabel(sess)
	if label == "-" {
		label = firstNonEmpty(stringPtrValue(thread.Name), thread.Preview, thread.ID)
	}
	bodyLines := []string{
		"当前 session: " + label,
		"session: `" + thread.ID + "`",
		fmt.Sprintf("turn 数: `%d`", total),
	}
	if total == 0 {
		bodyLines = append(bodyLines, "", "这个 Claude session 暂无可展示的 turn 记录。")
	} else {
		bodyLines = append(bodyLines, fmt.Sprintf("当前页: `%d-%d / %d`", start+1, end, total))
		for _, turn := range turns {
			if turn.IsCurrent {
				bodyLines = append(bodyLines, fmt.Sprintf("当前 turn: `Turn #%d`", turn.Ordinal))
				break
			}
		}
		bodyLines = append(bodyLines, "", "在线下拉菜单中选择要查看的 turn。")
	}

	buttons := make([]feishu.Button, 0, 3)
	selectOptions := make([]selectStaticOption, 0, end-start)
	initialOption := ""
	for idx := start; idx < end; idx++ {
		turn := turns[idx]
		label := fmt.Sprintf("Turn #%d | %s | %s", turn.Ordinal, firstNonEmpty(turn.Status, "-"), firstNonEmpty(turn.Preview, "-"))
		if turn.IsCurrent {
			label = "当前 · " + label
			initialOption = strconv.Itoa(idx)
		}
		selectOptions = append(selectOptions, selectStaticOption{
			Text:  truncate(label, 72),
			Value: strconv.Itoa(idx),
		})
	}
	if page > 0 {
		buttons = append(buttons, feishu.Button{
			Text: "上一页",
			Type: "default",
			Value: map[string]any{
				"action":      "history.page",
				"session_key": sessionKey,
				"page":        page - 1,
			},
		})
	}
	if end < total {
		buttons = append(buttons, feishu.Button{
			Text: "下一页",
			Type: "default",
			Value: map[string]any{
				"action":      "history.page",
				"session_key": sessionKey,
				"page":        page + 1,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: "返回上一级",
		Type: "default",
		Value: map[string]any{
			"action":      "menu.tools",
			"session_key": sessionKey,
		},
	})
	card := newMarkdownBodyCard("历史记录", "blue")
	appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": menuCardBody("menu.history", strings.Join(bodyLines, "\n"))})
	if len(selectOptions) > 0 {
		appendMarkdownBodyCardElement(card, buildSelectStaticElement(
			"history_detail_select",
			"选择要查看的 turn",
			map[string]any{"action": "history.detail.select", "session_key": sessionKey},
			selectOptions,
			initialOption,
		))
	}
	appendMarkdownBodyCardElement(card, buildMarkdownBodyCardActionElement(buttons))
	return card, nil
}

func (a *App) renderClaudeHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	sess, thread, turns, err := a.fetchClaudeCurrentSessionTurns(sessionKey)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(turns) {
		return nil, fmt.Errorf("history turn index out of range")
	}
	turn := turns[index]
	label := currentThreadLabel(sess)
	if label == "-" {
		label = firstNonEmpty(stringPtrValue(thread.Name), thread.Preview, thread.ID)
	}
	bodyLines := []string{
		"当前 session: " + label,
		"session: `" + thread.ID + "`",
		fmt.Sprintf("Turn #%d", turn.Ordinal),
		"turn_id: `" + firstNonEmpty(turn.TurnID, fmt.Sprintf("claude-turn-%d", turn.Ordinal)) + "`",
		"状态: `" + firstNonEmpty(turn.Status, "-") + "`",
		fmt.Sprintf("记录数: `%d`", len(turn.Records)),
		"",
		"原始记录：",
	}
	if len(turn.Records) == 0 {
		bodyLines = append(bodyLines, "-")
	} else {
		for idx, record := range turn.Records {
			meta := []string{"`" + firstNonEmpty(record.EntryType, "-") + "`"}
			if record.Timestamp != "" {
				meta = append(meta, "`"+record.Timestamp+"`")
			}
			bodyLines = append(bodyLines, fmt.Sprintf("%d. %s", idx+1, strings.Join(meta, " · ")))
			if record.PromptID != "" {
				bodyLines = append(bodyLines, "prompt_id: `"+record.PromptID+"`")
			}
			if record.MessageID != "" {
				bodyLines = append(bodyLines, "message_id: `"+record.MessageID+"`")
			}
			if record.StopReason != "" {
				bodyLines = append(bodyLines, "stop_reason: `"+record.StopReason+"`")
			}
			if len(record.Details) == 0 {
				bodyLines = append(bodyLines, "-")
				continue
			}
			for _, line := range record.Details {
				bodyLines = append(bodyLines, truncate(line, 600))
			}
		}
	}
	buttons := make([]feishu.Button, 0, 3)
	if index > 0 {
		buttons = append(buttons, feishu.Button{
			Text: "更新一条",
			Type: "default",
			Value: map[string]any{
				"action":      "history.detail",
				"session_key": sessionKey,
				"index":       index - 1,
			},
		})
	}
	if index+1 < len(turns) {
		buttons = append(buttons, feishu.Button{
			Text: "更旧一条",
			Type: "default",
			Value: map[string]any{
				"action":      "history.detail",
				"session_key": sessionKey,
				"index":       index + 1,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: "返回上一级",
		Type: "default",
		Value: map[string]any{
			"action":      "history.page",
			"session_key": sessionKey,
			"page":        index / historyPageSize,
		},
	})
	return a.feishu.SimpleStatusCard("Turn 详情", "blue", menuCardBody("history.detail", strings.Join(bodyLines, "\n")), buttons), nil
}

func (a *App) fetchClaudeCurrentSessionTurns(sessionKey string) (*state.Session, *codexrpc.ThreadReadThread, []claudeHistoryTurnSummary, error) {
	if a == nil || a.store == nil {
		return nil, nil, nil, fmt.Errorf("store not initialized")
	}
	sess := a.appState().session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, nil, nil, fmt.Errorf("当前没有活动线程")
	}
	filePath, meta, err := findClaudeSessionFile(strings.TrimSpace(sess.ActiveThreadID))
	if err != nil {
		return nil, nil, nil, err
	}
	if strings.TrimSpace(filePath) == "" {
		return nil, nil, nil, fmt.Errorf("未找到 Claude session `%s` 的本地 transcript", strings.TrimSpace(sess.ActiveThreadID))
	}
	turns, err := readClaudeHistoryTurns(filePath, sessionHasInFlightSubmission(sess))
	if err != nil {
		return nil, nil, nil, err
	}
	thread := &codexrpc.ThreadReadThread{ID: strings.TrimSpace(sess.ActiveThreadID)}
	if meta != nil {
		if title := strings.TrimSpace(meta.Title); title != "" {
			thread.Name = &title
		}
		thread.Preview = strings.TrimSpace(meta.Preview)
		thread.Cwd = strings.TrimSpace(meta.Cwd)
	}
	if thread.Name == nil {
		if name := strings.TrimSpace(sess.ActiveThreadName); name != "" {
			thread.Name = &name
		}
	}
	if strings.TrimSpace(thread.Preview) == "" {
		thread.Preview = strings.TrimSpace(sess.ActiveThreadPreview)
	}
	return sess, thread, turns, nil
}

func findClaudeSessionFile(sessionID string) (string, *claudeSessionListMeta, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", nil, nil
	}
	dirs, _, err := claudeSessionProjectDirs("", true)
	if err != nil {
		return "", nil, err
	}
	for _, dir := range dirs {
		filePath := filepath.Join(dir, sessionID+".jsonl")
		meta, err := readClaudeSessionListMeta(filePath)
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

func readClaudeHistoryTurns(filePath string, markLatestCurrent bool) ([]claudeHistoryTurnSummary, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	turns := make([]claudeHistoryTurnSummary, 0, 32)
	var current *claudeHistoryTurnSummary

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
		entry, ok := parseClaudeSessionLine(scanner.Text())
		if !ok {
			continue
		}
		record, include := claudeHistoryRecordFromEntry(entry)
		if !include {
			continue
		}
		promptID := strings.TrimSpace(record.PromptID)
		if promptID != "" && claudeHistoryStartsTurn(entry) && (current == nil || current.TurnID != promptID) {
			flushCurrent()
			current = &claudeHistoryTurnSummary{TurnID: promptID, Preview: record.Preview}
		}
		if current == nil {
			if promptID == "" {
				continue
			}
			current = &claudeHistoryTurnSummary{TurnID: promptID, Preview: record.Preview}
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

func claudeHistoryStartsTurn(entry map[string]any) bool {
	if strings.TrimSpace(claudeStringValue(entry["type"])) != "user" {
		return false
	}
	message, _ := entry["message"].(map[string]any)
	return claudeHistoryHasPromptContent(message["content"])
}

func claudeHistoryHasPromptContent(content any) bool {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value) != ""
	case []any:
		for _, blockValue := range value {
			block, _ := blockValue.(map[string]any)
			switch strings.TrimSpace(claudeStringValue(block["type"])) {
			case "text", "image":
				return true
			}
		}
	}
	return false
}

func claudeHistoryRecordFromEntry(entry map[string]any) (claudeHistoryRecord, bool) {
	if sidechain, _ := entry["isSidechain"].(bool); sidechain {
		return claudeHistoryRecord{}, false
	}
	recordType := strings.TrimSpace(claudeStringValue(entry["type"]))
	record := claudeHistoryRecord{
		EntryID:    strings.TrimSpace(claudeStringValue(entry["uuid"])),
		EntryType:  recordType,
		Timestamp:  strings.TrimSpace(claudeStringValue(entry["timestamp"])),
		PromptID:   strings.TrimSpace(claudeStringValue(entry["promptId"])),
		ParentUUID: strings.TrimSpace(claudeStringValue(entry["parentUuid"])),
	}
	switch recordType {
	case "user":
		message, _ := entry["message"].(map[string]any)
		record.Details = claudeHistoryRenderEntryContent(message["content"])
	case "assistant":
		message, _ := entry["message"].(map[string]any)
		record.MessageID = strings.TrimSpace(claudeStringValue(message["id"]))
		record.StopReason = strings.TrimSpace(claudeStringValue(message["stop_reason"]))
		record.Details = claudeHistoryRenderEntryContent(message["content"])
	case "system":
		if subtype := strings.TrimSpace(claudeStringValue(entry["subtype"])); subtype != "" {
			record.EntryType = "system:" + subtype
		}
		record.Details = claudeHistoryRenderSystemDetails(entry)
	case "max_turns_reached":
		record.Details = []string{"Claude 达到最大 turn 限制"}
	default:
		return claudeHistoryRecord{}, false
	}
	if len(record.Details) == 0 {
		record.Details = []string{record.EntryType}
	}
	record.Preview = historyInputPreview(record.Details)
	return record, true
}

func claudeHistoryRenderEntryContent(content any) []string {
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
			blockType := strings.TrimSpace(claudeStringValue(block["type"]))
			switch blockType {
			case "text":
				text := strings.TrimSpace(claudeStringValue(block["text"]))
				if text != "" {
					rendered = append(rendered, text)
				}
			case "tool_use":
				name := firstNonEmpty(strings.TrimSpace(claudeStringValue(block["name"])), "(unknown tool)")
				input, _ := block["input"].(map[string]any)
				detail := firstNonEmpty(strings.TrimSpace(claudeStringValue(input["command"])), strings.TrimSpace(claudeStringValue(input["description"])), strings.TrimSpace(claudeStringValue(input["prompt"])))
				if detail == "" {
					rendered = append(rendered, "[tool_use] "+name)
				} else {
					rendered = append(rendered, "[tool_use] "+name+": "+detail)
				}
			case "tool_result":
				text := strings.TrimSpace(claudeMessageText(block["content"]))
				if text == "" {
					text = strings.TrimSpace(claudeStringValue(block["content"]))
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

func claudeHistoryRenderSystemDetails(entry map[string]any) []string {
	details := []string{}
	if subtype := strings.TrimSpace(claudeStringValue(entry["subtype"])); subtype != "" {
		details = append(details, "subtype: "+subtype)
	}
	if cwd := strings.TrimSpace(claudeStringValue(entry["cwd"])); cwd != "" {
		details = append(details, "cwd: "+cwd)
	}
	if sessionID := strings.TrimSpace(claudeStringValue(entry["session_id"])); sessionID != "" {
		details = append(details, "session_id: "+sessionID)
	}
	return details
}
