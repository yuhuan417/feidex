package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"feidex/internal/state"
)

const (
	quietWorkingCardTitle    = "工作中"
	quietWorkingCardColor    = "blue"
	quietWorkingReasoningKey = "__reasoning__"
)

type quietWorkingCard struct {
	MessageID    string
	EntryOrder   []string
	Entries      map[string]string
	RenderedBody string
}

type quietWorkingCardOp struct {
	TurnID    string
	MessageID string
	Body      string
}

type quietWorkingBoundary struct {
	Op             quietWorkingCardOp
	ReuseMessageID string
}

func isQuietBoundaryTurnItem(itemType string) bool {
	switch normalizeTurnItemType(itemType) {
	case "agent_message", "plan", "exited_review_mode":
		return true
	default:
		return false
	}
}

func prepareQuietWorkingCardUpdateLocked(stream *turnStream, itemID string, item map[string]any, workspaceCwd string) quietWorkingCardOp {
	if stream == nil {
		return quietWorkingCardOp{}
	}
	prefix, lines := buildQuietWorkingCardLines(itemID, item, workspaceCwd)
	if prefix == "" {
		prefix = quietWorkingItemKey(itemID)
	}
	card := stream.QuietWorking
	if card == nil {
		if len(trimmedNonEmptyStrings(lines)) == 0 {
			return quietWorkingCardOp{}
		}
		card = &quietWorkingCard{Entries: map[string]string{}}
		stream.QuietWorking = card
	}
	changed := card.replaceEntries(prefix, lines)
	if prefix != quietWorkingReasoningKey && changed {
		if card.removeEntries(quietWorkingReasoningKey) {
			changed = true
		}
	}
	if !changed {
		return quietWorkingCardOp{}
	}
	body := card.body()
	if body == "" {
		return quietWorkingCardOp{}
	}
	if body == card.RenderedBody {
		return quietWorkingCardOp{}
	}
	return quietWorkingCardOp{
		TurnID:    stream.TurnID,
		MessageID: card.MessageID,
		Body:      body,
	}
}

func prepareQuietWorkingCardBoundaryLocked(stream *turnStream) quietWorkingBoundary {
	if stream == nil || stream.QuietWorking == nil {
		return quietWorkingBoundary{}
	}
	card := stream.QuietWorking
	if card.isReasoningOnly() {
		reuseMessageID := strings.TrimSpace(card.MessageID)
		stream.QuietWorking = nil
		return quietWorkingBoundary{ReuseMessageID: reuseMessageID}
	}
	var op quietWorkingCardOp
	if card.removeEntries(quietWorkingReasoningKey) && strings.TrimSpace(card.MessageID) != "" {
		body := card.body()
		if body != "" && body != card.RenderedBody {
			op = quietWorkingCardOp{
				TurnID:    stream.TurnID,
				MessageID: card.MessageID,
				Body:      body,
			}
		}
	}
	stream.QuietWorking = nil
	return quietWorkingBoundary{Op: op}
}

func executeQuietWorkingCardOp(a *App, ctx context.Context, sub *state.Submission, op quietWorkingCardOp) {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return
	}
	if strings.TrimSpace(op.Body) == "" {
		return
	}
	card := cardRendererForApp(a).renderCompactMarkdownCard(sub, quietWorkingCardTitle, quietWorkingCardColor, "", op.Body, nil)
	if strings.TrimSpace(op.MessageID) == "" {
		if strings.TrimSpace(op.Body) == "" {
			return
		}
		messageID, err := a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, replyInThreadForSubmission(a, sub))
		if err != nil || strings.TrimSpace(messageID) == "" {
			slog.Warn("send quiet working card failed",
				"turn_id", op.TurnID,
				"error", err,
			)
			return
		}
		recordMessageLink(a, messageID, "turn_working", sub, "")
		commitQuietWorkingCardRender(a, op.TurnID, messageID, op.Body)
		return
	}
	if err := a.feishu.PatchCard(ctx, op.MessageID, card); err != nil {
		slog.Warn("patch quiet working card failed",
			"turn_id", op.TurnID,
			"message_id", op.MessageID,
			"error", err,
		)
		return
	}
	commitQuietWorkingCardRender(a, op.TurnID, op.MessageID, op.Body)
}

func commitQuietWorkingCardRender(a *App, turnID, messageID, body string) {
	newTurnStreamService(a).commitTurnStreamQuietRender(turnID, messageID, body)
}

func buildQuietWorkingCardLines(itemID string, item map[string]any, workspaceCwd string) (string, []string) {
	itemType := normalizeTurnItemType(stringValue(item["type"]))
	switch itemType {
	case "reasoning":
		return quietWorkingReasoningKey, []string{"思考中..."}
	case "command_execution":
		return quietWorkingItemKey(itemID), buildQuietCommandExecutionLines(item, workspaceCwd)
	case "file_change":
		return quietWorkingItemKey(itemID), buildQuietFileChangeLines(item)
	case "web_search":
		return quietWorkingItemKey(itemID), buildQuietWebSearchLines(item)
	case "dynamic_tool_call":
		return quietWorkingItemKey(itemID), buildClaudeQuietDynamicToolLines(stringValue(item["tool"]), item["input"], workspaceCwd)
	case "context_compaction":
		return quietWorkingItemKey(itemID), []string{"Context compaction..."}
	default:
		return quietWorkingItemKey(itemID), nil
	}
}

func buildQuietCommandExecutionLines(item map[string]any, workspaceCwd string) []string {
	if normalizeWorkingStatus(item["status"]) != "completed" {
		return nil
	}
	actions, _ := item["commandActions"].([]any)
	if len(actions) == 0 {
		return nil
	}
	lines := make([]string, 0, len(actions))
	readNames := make([]string, 0, len(actions))
	readIndex := -1
	cwd := strings.TrimSpace(stringValue(item["cwd"]))
	for _, raw := range actions {
		action, _ := raw.(map[string]any)
		switch normalizeCommandActionType(action["type"]) {
		case "read":
			if readIndex < 0 {
				readIndex = len(lines)
			}
			name := quietDisplayFileName(firstNonEmpty(stringValue(action["name"]), stringValue(action["path"])))
			if name != "" {
				readNames = append(readNames, name)
			}
		case "listfiles":
			path := renderWorkspaceDisplayPath(firstNonEmpty(stringValue(action["path"]), cwd), workspaceCwd)
			if path == "" {
				path = "."
			}
			lines = append(lines, "List "+markdownInlineCode(path))
		case "search":
			path := renderWorkspaceDisplayPath(firstNonEmpty(stringValue(action["path"]), cwd), workspaceCwd)
			query := strings.TrimSpace(stringValue(action["query"]))
			lines = append(lines, buildQuietSearchLine(query, path))
		}
	}
	if len(readNames) > 0 {
		readLine := "Read " + strings.Join(markdownInlineCodeSlice(readNames), " ")
		if readIndex < 0 || readIndex >= len(lines) {
			lines = append(lines, readLine)
		} else {
			lines = append(lines, "")
			copy(lines[readIndex+1:], lines[readIndex:])
			lines[readIndex] = readLine
		}
	}
	return trimmedNonEmptyStrings(lines)
}

func buildQuietSearchLine(query, path string) string {
	query = strings.TrimSpace(query)
	path = strings.TrimSpace(path)
	switch {
	case query != "" && path != "":
		return "Search " + markdownInlineCode(query) + " in " + markdownInlineCode(path)
	case query != "":
		return "Search " + markdownInlineCode(query)
	case path != "":
		return "Search in " + markdownInlineCode(path)
	default:
		return ""
	}
}

func buildQuietFileChangeLines(item map[string]any) []string {
	if normalizeWorkingStatus(item["status"]) != "completed" {
		return nil
	}
	changes, _ := item["changes"].([]any)
	if len(changes) == 0 {
		return nil
	}
	adding := []string{}
	deleting := []string{}
	updating := []string{}
	for _, raw := range changes {
		change, _ := raw.(map[string]any)
		switch quietPatchChangeType(change["kind"]) {
		case "add":
			adding = append(adding, quietDisplayFileName(stringValue(change["path"])))
		case "delete":
			deleting = append(deleting, quietDisplayFileName(stringValue(change["path"])))
		case "update":
			updating = append(updating, quietDisplayFileName(stringValue(change["path"])))
			if movePath := quietPatchMovePath(change["kind"]); movePath != "" {
				updating = append(updating, quietDisplayFileName(movePath))
			}
		}
	}
	parts := make([]string, 0, 3)
	if names := markdownInlineCodeSlice(quietDedupeStrings(adding)); len(names) > 0 {
		parts = append(parts, "Add "+strings.Join(names, " "))
	}
	if names := markdownInlineCodeSlice(quietDedupeStrings(deleting)); len(names) > 0 {
		parts = append(parts, "Delete "+strings.Join(names, " "))
	}
	if names := markdownInlineCodeSlice(quietDedupeStrings(updating)); len(names) > 0 {
		parts = append(parts, "Update "+strings.Join(names, " "))
	}
	return parts
}

func buildQuietWebSearchLines(item map[string]any) []string {
	action, _ := item["action"].(map[string]any)
	actionType := normalizeWebSearchActionType("")
	if action != nil {
		actionType = normalizeWebSearchActionType(action["type"])
	}
	switch actionType {
	case "", "search":
		query := strings.TrimSpace(firstNonEmpty(
			stringValue(item["query"]),
			stringValue(action["query"]),
			joinQuietStringList(action["queries"]),
		))
		if query == "" {
			return nil
		}
		return []string{"Searching the web: " + markdownInlineCode(query)}
	case "findinpage":
		url := strings.TrimSpace(stringValue(action["url"]))
		if url == "" {
			return nil
		}
		return []string{"Find in page: " + markdownInlineCode(url)}
	default:
		return nil
	}
}

func quietWorkingItemKey(itemID string) string {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "__item__"
	}
	return "item:" + itemID
}

func (c *quietWorkingCard) replaceEntries(prefix string, lines []string) bool {
	if c == nil {
		return false
	}
	lines = trimmedNonEmptyStrings(lines)
	if equalStringSlices(c.linesForPrefix(prefix), lines) {
		return false
	}
	if c.Entries == nil {
		c.Entries = map[string]string{}
	}
	c.removeEntries(prefix)
	for i, line := range lines {
		key := quietWorkingEntryKey(prefix, i)
		c.EntryOrder = append(c.EntryOrder, key)
		c.Entries[key] = line
	}
	return true
}

func (c *quietWorkingCard) removeEntries(prefix string) bool {
	if c == nil || len(c.EntryOrder) == 0 {
		return false
	}
	changed := false
	order := make([]string, 0, len(c.EntryOrder))
	for _, key := range c.EntryOrder {
		if quietWorkingEntryPrefix(key) == prefix {
			delete(c.Entries, key)
			changed = true
			continue
		}
		order = append(order, key)
	}
	c.EntryOrder = order
	return changed
}

func (c *quietWorkingCard) linesForPrefix(prefix string) []string {
	if c == nil || len(c.EntryOrder) == 0 {
		return nil
	}
	lines := make([]string, 0, len(c.EntryOrder))
	for _, key := range c.EntryOrder {
		if quietWorkingEntryPrefix(key) != prefix {
			continue
		}
		line := strings.TrimSpace(c.Entries[key])
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func (c *quietWorkingCard) body() string {
	if c == nil || len(c.EntryOrder) == 0 {
		return ""
	}
	lines := make([]string, 0, len(c.EntryOrder))
	for _, key := range c.EntryOrder {
		line := strings.TrimSpace(c.Entries[key])
		if line != "" {
			lines = append(lines, line)
		}
	}
	lines = compactQuietWorkingLines(lines)
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	return fenceQuietWorkingBodyIfNeeded(text)
}

// fenceQuietWorkingBodyIfNeeded wraps text in a backtick code fence when it
// contains bare backticks that could break the outer markdown element boundary
// in Feishu cards. The fence length is one more than the longest consecutive
// backtick run in the content, mirroring the approach used by
// markdownCodeBlockWithLang for turn item cards.
func fenceQuietWorkingBodyIfNeeded(text string) string {
	if !strings.Contains(text, "`") {
		return text
	}
	n := maxConsecutiveBackticks(text) + 1
	if n < markdownFencePreferredLen {
		n = markdownFencePreferredLen
	}
	fence := strings.Repeat("`", n)
	return fence + "\n" + text + "\n" + fence
}

func compactQuietWorkingLines(lines []string) []string {
	lines = trimmedNonEmptyStrings(lines)
	if len(lines) <= 1 {
		return lines
	}
	compacted := make([]string, 0, len(lines))
	for _, line := range lines {
		verb, tail, ok := parseQuietMergeableLine(line)
		if !ok {
			compacted = append(compacted, line)
			continue
		}
		if len(compacted) == 0 {
			compacted = append(compacted, line)
			continue
		}
		lastVerb, lastTail, lastOK := parseQuietMergeableLine(compacted[len(compacted)-1])
		if !lastOK || lastVerb != verb {
			compacted = append(compacted, line)
			continue
		}
		mergedTail := strings.TrimSpace(strings.TrimSpace(lastTail) + " " + strings.TrimSpace(tail))
		compacted[len(compacted)-1] = strings.TrimSpace(verb + " " + mergedTail)
	}
	return compacted
}

func parseQuietMergeableLine(line string) (verb, tail string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	verb, tail, found := strings.Cut(line, " ")
	if !found {
		return "", "", false
	}
	switch strings.TrimSpace(verb) {
	case "Read", "List", "Add", "Delete", "Update":
		return strings.TrimSpace(verb), strings.TrimSpace(tail), true
	default:
		return "", "", false
	}
}

func (c *quietWorkingCard) isReasoningOnly() bool {
	if c == nil || len(c.EntryOrder) == 0 {
		return false
	}
	hasReasoning := false
	for _, key := range c.EntryOrder {
		line := strings.TrimSpace(c.Entries[key])
		if line == "" {
			continue
		}
		if quietWorkingEntryPrefix(key) != quietWorkingReasoningKey {
			return false
		}
		hasReasoning = true
	}
	return hasReasoning
}

func quietWorkingEntryKey(prefix string, index int) string {
	return prefix + "\x00" + fmt.Sprintf("%d", index)
}

func quietWorkingEntryPrefix(key string) string {
	if idx := strings.Index(key, "\x00"); idx >= 0 {
		return key[:idx]
	}
	return key
}

func normalizeWorkingStatus(v any) string {
	status := strings.ToLower(strings.TrimSpace(stringValue(v)))
	status = strings.ReplaceAll(status, "_", "")
	return status
}

func normalizeCommandActionType(v any) string {
	raw := strings.ToLower(strings.TrimSpace(stringValue(v)))
	raw = strings.ReplaceAll(raw, "_", "")
	raw = strings.ReplaceAll(raw, "-", "")
	return raw
}

func normalizeWebSearchActionType(v any) string {
	raw := strings.ToLower(strings.TrimSpace(stringValue(v)))
	raw = strings.ReplaceAll(raw, "_", "")
	raw = strings.ReplaceAll(raw, "-", "")
	return raw
}

func quietPatchChangeType(v any) string {
	switch x := v.(type) {
	case map[string]any:
		return strings.ToLower(strings.TrimSpace(firstNonEmpty(stringValue(x["type"]), stringValue(x["kind"]))))
	default:
		return strings.ToLower(strings.TrimSpace(stringValue(v)))
	}
}

func quietPatchMovePath(v any) string {
	change, _ := v.(map[string]any)
	if change == nil {
		return ""
	}
	return firstNonEmpty(stringValue(change["move_path"]), stringValue(change["movePath"]))
}

func quietDisplayFileName(path string) string {
	base, _ := splitPathLineReference(path)
	base = strings.TrimSpace(base)
	if base == "" {
		return strings.TrimSpace(path)
	}
	return filepath.Base(base)
}

func markdownInlineCode(s string) string {
	s = inlineCodeText(s)
	if s == "" {
		return ""
	}
	return "`" + s + "`"
}

func markdownInlineCodeSlice(values []string) []string {
	values = quietDedupeStrings(values)
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if code := markdownInlineCode(value); code != "" {
			parts = append(parts, code)
		}
	}
	return parts
}

func quietDedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func trimmedNonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func joinQuietStringList(v any) string {
	switch x := v.(type) {
	case []string:
		return strings.Join(trimmedNonEmptyStrings(x), " | ")
	case []any:
		values := make([]string, 0, len(x))
		for _, raw := range x {
			if text := strings.TrimSpace(stringValue(raw)); text != "" {
				values = append(values, text)
			}
		}
		return strings.Join(values, " | ")
	default:
		return ""
	}
}
