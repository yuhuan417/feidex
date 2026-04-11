package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"feidex/internal/state"
)

type turnStream struct {
	TurnID       string
	ThreadID     string
	SubmissionID string
	SessionKey   string
	WorkspaceID  string

	PendingPlan  string
	LastSentPlan string
	LastError    string
	SentOutput   bool
	SentFinal    bool
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
	ItemID        string
	ItemType      string
	StoreText     string
	SendText      string
	DetailText    string
	LinkKind      string
	IsOutput      bool
	Expandable    bool
	IsFinalAnswer bool
}

type turnStreamFlushResult struct {
	SentOutput bool
	SawFinal   bool
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
	if snapshot.IsFinalAnswer && stream.SentFinal {
		delete(stream.Items, key)
		a.turnStreamsMu.Unlock()
		return
	}
	delete(stream.Items, key)
	if snapshot.IsFinalAnswer {
		stream.SentFinal = true
	}
	a.turnStreamsMu.Unlock()

	if planText != "" {
		a.storePlanSnapshot(sub.ID, planText)
		a.sendPlanCard(ctx, sub, planText, true)
	}
	if snapshot.IsFinalAnswer {
		a.storeTurnItemSnapshot(sub.ID, snapshot)
		_ = a.noteTurnFirstFinal(turnID, snapshot.StoreText)
		return
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
	result.SawFinal = stream.SentFinal
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
	if strings.TrimSpace(snapshot.SendText) == "" && strings.TrimSpace(snapshot.DetailText) == "" {
		return false
	}
	id := a.sendTurnSnapshotCard(ctx, sub, snapshot, includeActions)
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

func (a *App) replyInThreadForSubmission(sub *state.Submission) bool {
	if sub == nil {
		return false
	}
	sess := a.store.GetSession(sub.SessionKey)
	return sess != nil && sess.ChatType == "group" && a.cfg.Feishu.ReplyInThread
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

type turnItemCardPayload struct {
	SubmissionID  string `json:"submission_id"`
	SessionKey    string `json:"session_key"`
	TurnID        string `json:"turn_id"`
	ItemID        string `json:"item_id"`
	ItemType      string `json:"item_type"`
	Title         string `json:"title"`
	Color         string `json:"color"`
	SummaryText   string `json:"summary_text"`
	DetailText    string `json:"detail_text"`
	LinkKind      string `json:"link_kind"`
	IsFinalAnswer bool   `json:"is_final_answer"`
}

func (a *App) sendTurnSnapshotCard(ctx context.Context, sub *state.Submission, snapshot turnItemSnapshot, includeActions bool) string {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	if a.quietModeEnabled() && !shouldDeliverTurnSnapshotInQuiet(snapshot) {
		return ""
	}
	title, color := turnSnapshotCardMeta(snapshot)
	payload := turnItemCardPayload{
		SubmissionID:  sub.ID,
		SessionKey:    sub.SessionKey,
		TurnID:        sub.TurnID,
		ItemID:        snapshot.ItemID,
		ItemType:      snapshot.ItemType,
		Title:         title,
		Color:         color,
		SummaryText:   strings.TrimSpace(snapshot.SendText),
		DetailText:    strings.TrimSpace(snapshot.DetailText),
		LinkKind:      snapshot.LinkKind,
		IsFinalAnswer: snapshot.IsFinalAnswer,
	}
	if isReplyTurnItem(payload.ItemType) {
		body := replyTurnItemCardBody(payload)
		if body == "" {
			body = payload.DetailText
		}
		results := a.sendReplyCardChunks(
			ctx,
			sub,
			replyTurnItemCardTitle(payload),
			payload.Color,
			buildReplyCardChunks(body, payload.IsFinalAnswer, nil),
			a.replyInThreadForSubmission(sub),
			snapshot.IsFinalAnswer,
		)
		if len(results) == 0 {
			fallback := payload.SummaryText
			if fallback == "" {
				fallback = payload.DetailText
			}
			if snapshot.IsFinalAnswer {
				a.sendFinalMessages(ctx, sub, fallback, a.replyInThreadForSubmission(sub))
			} else {
				a.sendTurnEventMessages(ctx, sub, fallback, a.replyInThreadForSubmission(sub), snapshot.LinkKind)
			}
			return ""
		}
		for _, result := range results {
			a.recordMessageLink(result.MessageID, snapshot.LinkKind, sub, snapshot.ItemID)
		}
		return results[0].MessageID
	}
	card := a.renderTurnItemCardWithOptions(ctx, sub, payload, false, false, "", snapshot.IsFinalAnswer)
	id, err := a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, a.replyInThreadForSubmission(sub))
	if err != nil || strings.TrimSpace(id) == "" {
		fallback := payload.SummaryText
		if fallback == "" {
			fallback = payload.DetailText
		}
		if snapshot.IsFinalAnswer {
			a.sendFinalMessages(ctx, sub, fallback, a.replyInThreadForSubmission(sub))
		} else {
			a.sendTurnEventMessages(ctx, sub, fallback, a.replyInThreadForSubmission(sub), snapshot.LinkKind)
		}
		return ""
	}
	a.recordMessageLink(id, snapshot.LinkKind, sub, snapshot.ItemID)
	return id
}

func (a *App) sendTurnEventCard(ctx context.Context, sub *state.Submission, title, color, body, kind string, includeActions bool, itemID string) string {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	if a.quietModeEnabled() && !shouldDeliverTurnKindInQuiet(kind) {
		return ""
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	card := a.renderCompactMarkdownCard(sub, title, color, "", body, nil)
	id, err := a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, a.replyInThreadForSubmission(sub))
	if err != nil || strings.TrimSpace(id) == "" {
		a.sendTurnEventMessages(ctx, sub, body, a.replyInThreadForSubmission(sub), kind)
		return ""
	}
	a.recordMessageLink(id, kind, sub, itemID)
	return id
}

func (a *App) renderTurnItemCard(sub *state.Submission, payload turnItemCardPayload, expanded bool, includeActions bool, requestID string) map[string]any {
	return a.renderTurnItemCardWithOptions(context.Background(), sub, payload, expanded, includeActions, requestID, false)
}

func (a *App) renderTurnItemCardWithOptions(ctx context.Context, sub *state.Submission, payload turnItemCardPayload, expanded bool, includeActions bool, requestID string, enablePreview bool) map[string]any {
	_ = expanded
	_ = includeActions
	_ = requestID
	if isReplyTurnItem(payload.ItemType) {
		return a.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, replyTurnItemCardTitle(payload), payload.Color, payload.IsFinalAnswer, replyTurnItemCardBody(payload), nil, enablePreview)
	}
	meta, body := compactTurnItemCardContent(payload)
	return a.renderCompactMarkdownCard(sub, payload.Title, payload.Color, meta, body, nil)
}

func isReplyTurnItem(itemType string) bool {
	return normalizeTurnItemType(itemType) == "agent_message"
}

func replyTurnItemCardBody(payload turnItemCardPayload) string {
	body := stripTurnItemCardHeading(payload.SummaryText, payload.Title, payload.ItemType)
	if body == "" {
		body = stripTurnItemCardHeading(payload.DetailText, payload.Title, payload.ItemType)
	}
	return body
}

func replyTurnItemCardTitle(payload turnItemCardPayload) string {
	if payload.IsFinalAnswer {
		return payload.Title
	}
	return ""
}

func compactTurnItemCardContent(payload turnItemCardPayload) (string, string) {
	summary := stripTurnItemCardHeading(payload.SummaryText, payload.Title, payload.ItemType)
	detail := stripTurnItemCardHeading(payload.DetailText, payload.Title, payload.ItemType)

	switch normalizeTurnItemType(payload.ItemType) {
	case "command_execution":
		body, meta := splitCompactMetaLine(summary)
		return meta, joinMarkdownSections(body, detail)
	case "mcp_tool_call", "dynamic_tool_call", "collab_agent_tool_call":
		body, meta := splitCompactMetaLine(summary)
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

func stripTurnItemCardHeading(text, title, itemType string) string {
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
	labels := []string{strings.TrimSpace(title), turnItemLabel(itemType)}
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

func splitCompactMetaLine(text string) (string, string) {
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

func joinMarkdownSections(parts ...string) string {
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

func turnSnapshotCardMeta(snapshot turnItemSnapshot) (string, string) {
	if snapshot.IsFinalAnswer {
		return "最终答复", "green"
	}
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
