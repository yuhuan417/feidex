package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

type turnStream struct {
	TurnID      string
	ThreadID    string
	SubmissionID string
	SessionKey  string
	WorkspaceID string

	PendingPlan  string
	LastSentPlan string
	LastError    string
	SentOutput   bool
	NextOrder    int
	Items        map[string]*turnItemBuffer
}

type turnItemBuffer struct {
	Key      string
	ItemID   string
	ItemType string
	Command  string
	Delta    string
	Order    int
}

type turnItemSnapshot struct {
	ItemID    string
	ItemType  string
	StoreText string
	SendText  string
	LinkKind  string
	IsOutput  bool
}

type turnStreamFlushResult struct {
	SentOutput bool
	LastError  string
}

func (a *App) noteTurnStarted(sessionKey string, sub *state.Submission) {
	if sub == nil || strings.TrimSpace(sub.TurnID) == "" {
		return
	}
	a.turnStreamsMu.Lock()
	defer a.turnStreamsMu.Unlock()
	a.ensureTurnStreamLocked(sessionKey, sub)
}

func (a *App) appendTurnItemDelta(threadID, turnID, itemID, itemType, delta string) {
	if strings.TrimSpace(delta) == "" {
		return
	}
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}

	a.turnStreamsMu.Lock()
	defer a.turnStreamsMu.Unlock()
	stream := a.ensureTurnStreamLocked(sessionKey, sub)
	key := turnItemKey(itemID, itemType)
	item := stream.ensureItemBuffer(key, itemID, itemType)
	item.Delta += delta
}

func (a *App) updatePendingPlan(turnID, plan string) {
	sessionKey, sub := a.findSubmissionByTurn("", turnID)
	if sub == nil {
		return
	}
	a.turnStreamsMu.Lock()
	defer a.turnStreamsMu.Unlock()
	stream := a.ensureTurnStreamLocked(sessionKey, sub)
	stream.PendingPlan = strings.TrimSpace(plan)
}

func (a *App) recordTurnError(threadID, turnID, message string) {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	a.turnStreamsMu.Lock()
	defer a.turnStreamsMu.Unlock()
	stream := a.ensureTurnStreamLocked(sessionKey, sub)
	stream.LastError = strings.TrimSpace(message)
}

func (a *App) completeTurnItem(ctx context.Context, threadID, turnID, itemID string, item map[string]any) {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}

	var (
		planText string
		snapshot turnItemSnapshot
	)

	a.turnStreamsMu.Lock()
	stream := a.ensureTurnStreamLocked(sessionKey, sub)
	if strings.TrimSpace(threadID) != "" {
		stream.ThreadID = threadID
	}
	if text := strings.TrimSpace(stream.PendingPlan); text != "" && text != stream.LastSentPlan {
		planText = text
		stream.LastSentPlan = text
		stream.PendingPlan = ""
	}
	itemType := normalizeTurnItemType(stringValue(item["type"]))
	key := turnItemKey(itemID, itemType)
	buf := stream.ensureItemBuffer(key, itemID, itemType)
	if command := firstNonEmpty(stringValue(item["command"]), stringValue(item["commandLine"])); command != "" {
		buf.Command = command
	}
	snapshot = snapshotTurnItem(buf, item, false)
	delete(stream.Items, key)
	a.turnStreamsMu.Unlock()

	if planText != "" {
		a.storePlanSnapshot(sub.ID, planText)
		a.sendPlanCard(ctx, sub, planText, true)
	}
	a.deliverTurnItemSnapshot(ctx, sub, snapshot, true)
}

func (a *App) flushTurnStream(ctx context.Context, threadID, turnID string) turnStreamFlushResult {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		a.turnStreamsMu.Lock()
		delete(a.turnStreams, turnID)
		a.turnStreamsMu.Unlock()
		return turnStreamFlushResult{}
	}

	var (
		planText string
		items    []turnItemSnapshot
		result   turnStreamFlushResult
	)

	a.turnStreamsMu.Lock()
	stream := a.ensureTurnStreamLocked(sessionKey, sub)
	result.SentOutput = stream.SentOutput
	result.LastError = stream.LastError
	if text := strings.TrimSpace(stream.PendingPlan); text != "" && text != stream.LastSentPlan {
		planText = text
	}
	pending := make([]*turnItemBuffer, 0, len(stream.Items))
	for _, item := range stream.Items {
		pending = append(pending, item)
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Order < pending[j].Order })
	for _, item := range pending {
		items = append(items, snapshotTurnItem(item, nil, true))
	}
	delete(a.turnStreams, turnID)
	a.turnStreamsMu.Unlock()

	if planText != "" {
		a.storePlanSnapshot(sub.ID, planText)
		a.sendPlanCard(ctx, sub, planText, false)
	}
	for _, item := range items {
		if sent := a.deliverTurnItemSnapshot(ctx, sub, item, false); sent && item.IsOutput {
			result.SentOutput = true
		}
	}
	return result
}

func (a *App) deliverTurnItemSnapshot(ctx context.Context, sub *state.Submission, snapshot turnItemSnapshot, includeActions bool) bool {
	if sub == nil {
		return false
	}
	a.storeTurnItemSnapshot(sub.ID, snapshot)
	if strings.TrimSpace(snapshot.SendText) == "" {
		return false
	}
	title, color := turnSnapshotCardMeta(snapshot)
	id := a.sendTurnEventCard(ctx, sub, title, color, snapshot.SendText, snapshot.LinkKind, includeActions, snapshot.ItemID)
	ids := []string{}
	if strings.TrimSpace(id) != "" {
		ids = append(ids, id)
	}
	if snapshot.IsOutput && len(ids) > 0 {
		a.turnStreamsMu.Lock()
		if stream := a.turnStreams[sub.TurnID]; stream != nil {
			stream.SentOutput = true
		}
		a.turnStreamsMu.Unlock()
	}
	return len(ids) > 0
}

func (a *App) storePlanSnapshot(submissionID, plan string) {
	if strings.TrimSpace(submissionID) == "" {
		return
	}
	_ = a.store.UpdateSubmission(submissionID, func(sub *state.Submission) {
		sub.PlanText = strings.TrimSpace(plan)
	})
}

func (a *App) storeTurnItemSnapshot(submissionID string, snapshot turnItemSnapshot) {
	if strings.TrimSpace(submissionID) == "" || strings.TrimSpace(snapshot.StoreText) == "" {
		return
	}
	_ = a.store.UpdateSubmission(submissionID, func(sub *state.Submission) {
		switch snapshot.ItemType {
		case "reasoning":
			sub.SummaryText = appendSeparatedText(sub.SummaryText, snapshot.StoreText)
		case "command_execution":
			sub.CommandText = appendSeparatedText(sub.CommandText, snapshot.StoreText)
		case "agent_message":
			sub.OutputText = appendSeparatedText(sub.OutputText, snapshot.StoreText)
		}
	})
}

func (a *App) ensureTurnStreamLocked(sessionKey string, sub *state.Submission) *turnStream {
	if a.turnStreams == nil {
		a.turnStreams = map[string]*turnStream{}
	}
	stream := a.turnStreams[sub.TurnID]
	if stream != nil {
		stream.SessionKey = sessionKey
		stream.SubmissionID = sub.ID
		stream.WorkspaceID = sub.WorkspaceID
		if strings.TrimSpace(sub.ThreadID) != "" {
			stream.ThreadID = sub.ThreadID
		}
		return stream
	}
	stream = &turnStream{
		TurnID:       sub.TurnID,
		ThreadID:     sub.ThreadID,
		SubmissionID: sub.ID,
		SessionKey:   sessionKey,
		WorkspaceID:  sub.WorkspaceID,
		Items:        map[string]*turnItemBuffer{},
	}
	a.turnStreams[sub.TurnID] = stream
	return stream
}

func (s *turnStream) ensureItemBuffer(key, itemID, itemType string) *turnItemBuffer {
	if s.Items == nil {
		s.Items = map[string]*turnItemBuffer{}
	}
	if item := s.Items[key]; item != nil {
		if item.ItemID == "" {
			item.ItemID = strings.TrimSpace(itemID)
		}
		if item.ItemType == "" {
			item.ItemType = normalizeTurnItemType(itemType)
		}
		return item
	}
	s.NextOrder++
	item := &turnItemBuffer{
		Key:      key,
		ItemID:   strings.TrimSpace(itemID),
		ItemType: normalizeTurnItemType(itemType),
		Order:    s.NextOrder,
	}
	s.Items[key] = item
	return item
}

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
	case "reasoning":
		text := firstNonEmpty(extractTurnItemText(item, "summary", "summary_text"), strings.TrimSpace(deltaText(buf)), stringValue(item["text"]))
		return turnItemSnapshot{
			ItemID:    itemIDValue(buf, item),
			ItemType:  itemType,
			StoreText: text,
			SendText:  buildLabeledTurnEventText("思考", text, partial),
			LinkKind:  "turn_reasoning",
		}
	case "agent_message":
		text := firstNonEmpty(extractTurnItemText(item, "content", "output_text"), strings.TrimSpace(deltaText(buf)), stringValue(item["text"]))
		label := ""
		if partial {
			label = "回复（未完成）"
		}
		sendText := strings.TrimSpace(text)
		if label != "" {
			sendText = buildLabeledTurnEventText(label, text, false)
		}
		return turnItemSnapshot{
			ItemID:    itemIDValue(buf, item),
			ItemType:  itemType,
			StoreText: strings.TrimSpace(text),
			SendText:  sendText,
			LinkKind:  "turn_output",
			IsOutput:  true,
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
		return turnItemSnapshot{
			ItemID:    itemIDValue(buf, item),
			ItemType:  itemType,
			StoreText: strings.TrimSpace(firstNonEmpty(output, formatTurnCommandEvent(command, output, status, nil, partial))),
			SendText:  formatTurnCommandEvent(command, output, status, optionalIntPointer(exitCode, hasExitCode), partial),
			LinkKind:  "turn_command_execution",
		}
	case "file_change":
		text := firstNonEmpty(
			stringValue(item["aggregated_output"]),
			stringValue(item["aggregatedOutput"]),
			stringValue(item["output"]),
			extractTurnItemText(item, "content", "output_text"),
			extractTurnItemText(item, "summary", "summary_text"),
			strings.TrimSpace(deltaText(buf)),
		)
		return turnItemSnapshot{
			ItemID:    itemIDValue(buf, item),
			ItemType:  itemType,
			StoreText: strings.TrimSpace(text),
			SendText:  buildLabeledTurnEventText("文件改动", text, partial),
			LinkKind:  "turn_file_change",
		}
	default:
		text := firstNonEmpty(
			stringValue(item["output"]),
			extractTurnItemText(item, "content", "output_text"),
			extractTurnItemText(item, "summary", "summary_text"),
			stringValue(item["text"]),
			strings.TrimSpace(deltaText(buf)),
		)
		if strings.TrimSpace(text) == "" {
			return turnItemSnapshot{}
		}
		return turnItemSnapshot{
			ItemID:    itemIDValue(buf, item),
			ItemType:  itemType,
			StoreText: strings.TrimSpace(text),
			SendText:  buildLabeledTurnEventText(turnItemLabel(itemType), text, partial),
			LinkKind:  "turn_item",
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
		lines = append(lines, "$ "+strings.TrimSpace(command))
	}
	output = strings.TrimSpace(output)
	if output != "" {
		lines = append(lines, output)
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
	itemType = strings.ReplaceAll(itemType, "AgentMessage", "agent_message")
	itemType = strings.ReplaceAll(itemType, "CommandExecution", "command_execution")
	itemType = strings.ReplaceAll(itemType, "FileChange", "file_change")
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

func (a *App) replyInThreadForSubmission(sub *state.Submission) bool {
	if sub == nil {
		return false
	}
	sess := a.store.GetSession(sub.SessionKey)
	return sess != nil && sess.ChatType == "group" && a.cfg.Feishu.ReplyInThread
}

func (a *App) sendSubmissionStartedNotice(ctx context.Context, sub *state.Submission) {
	if sub == nil {
		return
	}
	body := "任务已开始，可在此卡片上中断。\n\n"
	if preview := strings.TrimSpace(submissionInputPreview(sub)); preview != "" && preview != "-" {
		body += "输入:\n" + preview + "\n\n"
	}
	body += "turn: `" + strings.TrimSpace(sub.TurnID) + "`"
	card := a.feishu.SimpleStatusCard("任务已开始", "blue", body, []feishu.Button{
		{
			Text: "中断",
			Type: "danger",
			Value: map[string]any{
				"action":      "menu.interrupt",
				"session_key": sub.SessionKey,
				"turn_id":     sub.TurnID,
			},
		},
		{
			Text: "线程列表",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.threads",
				"session_key": sub.SessionKey,
			},
		},
	})
	id, err := a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, a.replyInThreadForSubmission(sub))
	if err != nil || strings.TrimSpace(id) == "" {
		a.sendTurnEventMessages(ctx, sub, "开始处理。", a.replyInThreadForSubmission(sub), "turn_started")
		return
	}
	a.recordMessageLink(id, "turn_entry_card", sub, "")
}

func (a *App) sendSubmissionQueuedNotice(ctx context.Context, sub *state.Submission) {
	if sub == nil {
		return
	}
	a.sendTurnEventMessages(ctx, sub, "已加入队列，等待当前任务结束后开始处理。", a.replyInThreadForSubmission(sub), "turn_queued")
}

func (a *App) sendPlanCard(ctx context.Context, sub *state.Submission, planText string, includeActions bool) string {
	return a.sendTurnEventCard(ctx, sub, "计划更新", "blue", "计划:\n"+strings.TrimSpace(planText), "turn_plan", includeActions, "")
}

func (a *App) sendTurnEventCard(ctx context.Context, sub *state.Submission, title, color, body, kind string, includeActions bool, itemID string) string {
	if sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	body = truncate(body, 2600)
	body = a.prepareSubmissionCardMarkdown(sub, body)
	var buttons []feishu.Button
	if includeActions {
		buttons = turnActionButtons(sub, itemID)
	}
	card := a.feishu.SimpleStatusCard(title, color, body, buttons)
	id, err := a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, a.replyInThreadForSubmission(sub))
	if err != nil || strings.TrimSpace(id) == "" {
		a.sendTurnEventMessages(ctx, sub, body, a.replyInThreadForSubmission(sub), kind)
		return ""
	}
	a.recordMessageLink(id, kind, sub, itemID)
	return id
}

func turnActionButtons(sub *state.Submission, itemID string) []feishu.Button {
	if sub == nil || strings.TrimSpace(sub.TurnID) == "" {
		return nil
	}
	appendValue := map[string]any{
		"action":      "turn.append",
		"session_key": sub.SessionKey,
		"turn_id":     sub.TurnID,
	}
	if strings.TrimSpace(itemID) != "" {
		appendValue["item_id"] = itemID
	}
	return []feishu.Button{
		{Text: "追加", Type: "primary", Value: appendValue},
		{Text: "停止", Type: "danger", Value: map[string]any{"action": "menu.interrupt", "session_key": sub.SessionKey, "turn_id": sub.TurnID}},
	}
}

func turnSnapshotCardMeta(snapshot turnItemSnapshot) (string, string) {
	switch snapshot.ItemType {
	case "reasoning":
		return "思考", "grey"
	case "command_execution":
		return "命令执行", "blue"
	case "file_change":
		return "文件改动", "orange"
	case "agent_message":
		return "回复", "green"
	default:
		return turnItemLabel(snapshot.ItemType), "blue"
	}
}
