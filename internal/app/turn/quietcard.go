// Package turn provides pure turn-stream data types and quiet-working-card
// formatting functions extracted from the app package.
package turn

import (
	"fmt"
	"strings"

	"feidex/internal/app/apputil"
	"feidex/internal/app/turnitem"
	"feidex/internal/pathdisplay"
)

// QuietWorkingCardTitle is the title used for quiet working cards.
const QuietWorkingCardTitle = "工作中"

// QuietWorkingCardColor is the color used for quiet working cards.
const QuietWorkingCardColor = "blue"

// QuietWorkingReasoningKey is the entry key prefix used for reasoning items.
const QuietWorkingReasoningKey = "__reasoning__"

// QuietWorkingCard tracks incremental working-card entries for a single turn.
type QuietWorkingCard struct {
	MessageID    string
	EntryOrder   []string
	Entries      map[string]string
	DedupKeys    map[string]string // entry key → dedup key (full path) for deduplication
	RenderedBody string
}

// QuietWorkingCardOp represents a pending card send/patch operation.
type QuietWorkingCardOp struct {
	TurnID    string
	MessageID string
	Body      string
}

// QuietWorkingBoundary captures the card state at a turn-item boundary
// (e.g. agent_message, plan, exited_review_mode).
type QuietWorkingBoundary struct {
	Op             QuietWorkingCardOp
	ReuseMessageID string
}

// StreamState carries the subset of turn-stream fields needed by the
// quiet-card functions, decoupling them from the concrete turnStream type.
type StreamState struct {
	TurnID       string
	QuietWorking *QuietWorkingCard
}

// IsQuietBoundaryTurnItem returns true if the item type marks a quiet-card boundary.
func IsQuietBoundaryTurnItem(itemType string) bool {
	switch turnitem.NormalizeTurnItemType(itemType) {
	case "agent_message", "plan", "exited_review_mode":
		return true
	default:
		return false
	}
}

// PrepareUpdateLocked computes the next working-card operation for the given item.
// The caller must hold the stream tracker mutex.
func PrepareUpdateLocked(stream *StreamState, itemID string, item map[string]any, workspaceCwd string) QuietWorkingCardOp {
	if stream == nil {
		return QuietWorkingCardOp{}
	}
	prefix, lines := BuildWorkingCardLines(itemID, item, workspaceCwd)
	if prefix == "" {
		prefix = WorkingItemKey(itemID)
	}
	card := stream.QuietWorking
	if card == nil {
		if len(turnitem.TrimmedNonEmptyStrings(lines)) == 0 {
			return QuietWorkingCardOp{}
		}
		card = &QuietWorkingCard{Entries: map[string]string{}}
		stream.QuietWorking = card
	}
	changed := card.ReplaceEntries(prefix, lines)
	// Store dedup keys (full path) for dynamic tool calls so that
	// CompactQuietWorkingLines can deduplicate by actual file identity.
	if changed && turnitem.NormalizeTurnItemType(turnitem.StringValue(item["type"])) == "dynamic_tool_call" {
		dedupPath := turnitem.DynamicToolPath(item["input"], workspaceCwd)
		if dedupPath != "" {
			if card.DedupKeys == nil {
				card.DedupKeys = map[string]string{}
			}
			for i := range turnitem.TrimmedNonEmptyStrings(lines) {
				card.DedupKeys[EntryKey(prefix, i)] = dedupPath
			}
		}
	}
	if prefix != QuietWorkingReasoningKey && changed {
		if card.RemoveEntries(QuietWorkingReasoningKey) {
			changed = true
		}
	}
	if !changed {
		return QuietWorkingCardOp{}
	}
	body := card.Body()
	if body == "" {
		return QuietWorkingCardOp{}
	}
	if body == card.RenderedBody {
		return QuietWorkingCardOp{}
	}
	return QuietWorkingCardOp{
		TurnID:    stream.TurnID,
		MessageID: card.MessageID,
		Body:      body,
	}
}

// PrepareBoundaryLocked captures the card state at a turn-item boundary and
// resets the working card. The caller must hold the stream tracker mutex.
func PrepareBoundaryLocked(stream *StreamState) QuietWorkingBoundary {
	if stream == nil || stream.QuietWorking == nil {
		return QuietWorkingBoundary{}
	}
	card := stream.QuietWorking
	if card.IsReasoningOnly() {
		reuseMessageID := strings.TrimSpace(card.MessageID)
		stream.QuietWorking = nil
		return QuietWorkingBoundary{ReuseMessageID: reuseMessageID}
	}
	var op QuietWorkingCardOp
	if card.RemoveEntries(QuietWorkingReasoningKey) && strings.TrimSpace(card.MessageID) != "" {
		body := card.Body()
		if body != "" && body != card.RenderedBody {
			op = QuietWorkingCardOp{
				TurnID:    stream.TurnID,
				MessageID: card.MessageID,
				Body:      body,
			}
		}
	}
	stream.QuietWorking = nil
	return QuietWorkingBoundary{Op: op}
}

// BuildWorkingCardLines produces the entry prefix and lines for a turn item.
func BuildWorkingCardLines(itemID string, item map[string]any, workspaceCwd string) (string, []string) {
	itemType := turnitem.NormalizeTurnItemType(turnitem.StringValue(item["type"]))
	switch itemType {
	case "reasoning":
		return QuietWorkingReasoningKey, []string{"思考中..."}
	case "command_execution":
		return WorkingItemKey(itemID), BuildQuietCommandExecutionLines(item, workspaceCwd)
	case "file_change":
		return WorkingItemKey(itemID), buildQuietFileChangeLines(item)
	case "web_search":
		return WorkingItemKey(itemID), BuildQuietWebSearchLines(item)
	case "mcp_tool_call":
		toolName := strings.Trim(strings.TrimSpace(turnitem.StringValue(item["server"]))+"/"+strings.TrimSpace(turnitem.StringValue(item["tool"])), "/")
		lines := turnitem.BuildQuietMCPToolProgressLines(toolName, turnitem.ToolCallInput(item), workspaceCwd)
		if message := strings.TrimSpace(turnitem.StringValue(item["message"])); message != "" {
			lines = append(lines, message)
		}
		return WorkingItemKey(itemID), lines
	case "dynamic_tool_call":
		return WorkingItemKey(itemID), turnitem.BuildClaudeQuietDynamicToolLines(turnitem.StringValue(item["tool"]), item["input"], workspaceCwd)
	case "context_compaction":
		return WorkingItemKey(itemID), []string{"Context compaction..."}
	default:
		return WorkingItemKey(itemID), nil
	}
}

// WorkingItemKey builds the entry key for a turn item.
func WorkingItemKey(itemID string) string {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "__item__"
	}
	return "item:" + itemID
}

// ReplaceEntries replaces all lines for the given prefix. Returns true if the card changed.
func (c *QuietWorkingCard) ReplaceEntries(prefix string, lines []string) bool {
	if c == nil {
		return false
	}
	lines = turnitem.TrimmedNonEmptyStrings(lines)
	if EqualStringSlices(c.LinesForPrefix(prefix), lines) {
		return false
	}
	if c.Entries == nil {
		c.Entries = map[string]string{}
	}
	c.RemoveEntries(prefix)
	for i, line := range lines {
		key := EntryKey(prefix, i)
		c.EntryOrder = append(c.EntryOrder, key)
		c.Entries[key] = line
	}
	return true
}

// RemoveEntries removes all entries for the given prefix. Returns true if any were removed.
func (c *QuietWorkingCard) RemoveEntries(prefix string) bool {
	if c == nil || len(c.EntryOrder) == 0 {
		return false
	}
	changed := false
	order := make([]string, 0, len(c.EntryOrder))
	for _, key := range c.EntryOrder {
		if EntryPrefix(key) == prefix {
			delete(c.Entries, key)
			if c.DedupKeys != nil {
				delete(c.DedupKeys, key)
			}
			changed = true
			continue
		}
		order = append(order, key)
	}
	c.EntryOrder = order
	return changed
}

// LinesForPrefix returns the lines associated with the given prefix.
func (c *QuietWorkingCard) LinesForPrefix(prefix string) []string {
	if c == nil || len(c.EntryOrder) == 0 {
		return nil
	}
	lines := make([]string, 0, len(c.EntryOrder))
	for _, key := range c.EntryOrder {
		if EntryPrefix(key) != prefix {
			continue
		}
		line := strings.TrimSpace(c.Entries[key])
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// Body renders the card body from all entries.
func (c *QuietWorkingCard) Body() string {
	if c == nil || len(c.EntryOrder) == 0 {
		return ""
	}
	lines := make([]string, 0, len(c.EntryOrder))
	var dedupKeys []string
	for _, key := range c.EntryOrder {
		line := strings.TrimSpace(c.Entries[key])
		if line != "" {
			lines = append(lines, line)
			if c.DedupKeys != nil {
				dedupKeys = append(dedupKeys, c.DedupKeys[key])
			} else {
				dedupKeys = append(dedupKeys, "")
			}
		}
	}
	if c.DedupKeys != nil {
		lines = CompactQuietWorkingLinesWithDedup(lines, dedupKeys)
	} else {
		lines = CompactQuietWorkingLines(lines)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// IsReasoningOnly returns true if the card contains only reasoning entries.
func (c *QuietWorkingCard) IsReasoningOnly() bool {
	if c == nil || len(c.EntryOrder) == 0 {
		return false
	}
	hasReasoning := false
	for _, key := range c.EntryOrder {
		line := strings.TrimSpace(c.Entries[key])
		if line == "" {
			continue
		}
		if EntryPrefix(key) != QuietWorkingReasoningKey {
			return false
		}
		hasReasoning = true
	}
	return hasReasoning
}

// --- internal helpers ---

// EntryKey builds a composite key for a card entry.
func EntryKey(prefix string, index int) string {
	return prefix + "\x00" + fmt.Sprintf("%d", index)
}

// EntryPrefix extracts the prefix from a composite entry key.
func EntryPrefix(key string) string {
	if idx := strings.Index(key, "\x00"); idx >= 0 {
		return key[:idx]
	}
	return key
}

// BuildQuietCommandExecutionLines formats command execution items for quiet display.
func BuildQuietCommandExecutionLines(item map[string]any, workspaceCwd string) []string {
	if NormalizeWorkingStatus(item["status"]) != "completed" {
		return nil
	}
	actions, _ := item["commandActions"].([]any)
	if len(actions) == 0 {
		return nil
	}
	lines := make([]string, 0, len(actions))
	readNames := make([]string, 0, len(actions))
	readIndex := -1
	cwd := strings.TrimSpace(turnitem.StringValue(item["cwd"]))
	for _, raw := range actions {
		action, _ := raw.(map[string]any)
		switch NormalizeCommandActionType(action["type"]) {
		case "read":
			if readIndex < 0 {
				readIndex = len(lines)
			}
			name := turnitem.QuietDisplayFileName(apputil.FirstNonEmpty(turnitem.StringValue(action["name"]), turnitem.StringValue(action["path"])))
			if name != "" {
				readNames = append(readNames, name)
			}
		case "listfiles":
			path := pathdisplay.RenderWorkspaceDisplayPath(apputil.FirstNonEmpty(turnitem.StringValue(action["path"]), cwd), workspaceCwd)
			if path == "" {
				path = "."
			}
			lines = append(lines, "List "+turnitem.MarkdownInlineCode(path))
		case "search":
			path := pathdisplay.RenderWorkspaceDisplayPath(apputil.FirstNonEmpty(turnitem.StringValue(action["path"]), cwd), workspaceCwd)
			query := strings.TrimSpace(turnitem.StringValue(action["query"]))
			lines = append(lines, turnitem.BuildQuietSearchLine(query, path))
		}
	}
	if len(readNames) > 0 {
		readLine := "Read " + strings.Join(MarkdownInlineCodeSlice(readNames), " ")
		if readIndex < 0 || readIndex >= len(lines) {
			lines = append(lines, readLine)
		} else {
			lines = append(lines, "")
			copy(lines[readIndex+1:], lines[readIndex:])
			lines[readIndex] = readLine
		}
	}
	return turnitem.TrimmedNonEmptyStrings(lines)
}

func buildQuietFileChangeLines(item map[string]any) []string {
	if NormalizeWorkingStatus(item["status"]) != "completed" {
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
		switch QuietPatchChangeType(change["kind"]) {
		case "add":
			adding = append(adding, turnitem.QuietDisplayFileName(turnitem.StringValue(change["path"])))
		case "delete":
			deleting = append(deleting, turnitem.QuietDisplayFileName(turnitem.StringValue(change["path"])))
		case "update":
			updating = append(updating, turnitem.QuietDisplayFileName(turnitem.StringValue(change["path"])))
			if movePath := QuietPatchMovePath(change["kind"]); movePath != "" {
				updating = append(updating, turnitem.QuietDisplayFileName(movePath))
			}
		}
	}
	parts := make([]string, 0, 3)
	if names := MarkdownInlineCodeSlice(QuietDedupeStrings(adding)); len(names) > 0 {
		parts = append(parts, "Add "+strings.Join(names, " "))
	}
	if names := MarkdownInlineCodeSlice(QuietDedupeStrings(deleting)); len(names) > 0 {
		parts = append(parts, "Delete "+strings.Join(names, " "))
	}
	if names := MarkdownInlineCodeSlice(QuietDedupeStrings(updating)); len(names) > 0 {
		parts = append(parts, "Update "+strings.Join(names, " "))
	}
	return parts
}

// BuildQuietWebSearchLines formats web search items for quiet display.
func BuildQuietWebSearchLines(item map[string]any) []string {
	action, _ := item["action"].(map[string]any)
	actionType := NormalizeWebSearchActionType("")
	if action != nil {
		actionType = NormalizeWebSearchActionType(action["type"])
	}
	switch actionType {
	case "", "search":
		query := strings.TrimSpace(apputil.FirstNonEmpty(
			turnitem.StringValue(item["query"]),
			turnitem.StringValue(action["query"]),
			JoinQuietStringList(action["queries"]),
		))
		if query == "" {
			return nil
		}
		return []string{"Searching the web: " + turnitem.MarkdownInlineCode(query)}
	case "findinpage":
		url := strings.TrimSpace(turnitem.StringValue(action["url"]))
		if url == "" {
			return nil
		}
		return []string{"Find in page: " + turnitem.MarkdownInlineCode(url)}
	default:
		return nil
	}
}

// CompactQuietWorkingLines merges consecutive lines with the same verb prefix.
func CompactQuietWorkingLines(lines []string) []string {
	return CompactQuietWorkingLinesWithDedup(lines, nil)
}

// CompactQuietWorkingLinesWithDedup merges adjacent lines with the same verb,
// deduplicating by dedup key (full path) when available. Without dedup keys,
// falls back to display-name deduplication.
func CompactQuietWorkingLinesWithDedup(lines []string, dedupKeys []string) []string {
	lines = turnitem.TrimmedNonEmptyStrings(lines)
	if len(lines) <= 1 {
		return lines
	}
	if len(dedupKeys) > len(lines) {
		dedupKeys = dedupKeys[:len(lines)]
	}

	compacted := make([]string, 0, len(lines))
	compactedDedup := make([][]string, 0, len(lines))

	for i, line := range lines {
		verb, tail, ok := ParseQuietMergeableLine(line)
		if !ok {
			compacted = append(compacted, line)
			compactedDedup = append(compactedDedup, nil)
			continue
		}
		var myKey string
		if i < len(dedupKeys) {
			myKey = strings.TrimSpace(dedupKeys[i])
		}
		if len(compacted) == 0 {
			compacted = append(compacted, line)
			if myKey != "" {
				compactedDedup = append(compactedDedup, []string{myKey})
			} else {
				compactedDedup = append(compactedDedup, nil)
			}
			continue
		}
		lastVerb, lastTail, lastOK := ParseQuietMergeableLine(compacted[len(compacted)-1])
		if !lastOK || lastVerb != verb {
			compacted = append(compacted, line)
			if myKey != "" {
				compactedDedup = append(compactedDedup, []string{myKey})
			} else {
				compactedDedup = append(compactedDedup, nil)
			}
			continue
		}
		// Same verb — merge with dedup key awareness.
		if myKey != "" {
			seen := compactedDedup[len(compactedDedup)-1]
			dup := false
			for _, k := range seen {
				if k == myKey {
					dup = true
					break
				}
			}
			if dup {
				continue // same underlying file, skip
			}
			compactedDedup[len(compactedDedup)-1] = append(seen, myKey)
			// Different file — just concatenate tails.
			mergedTail := strings.TrimSpace(strings.TrimSpace(lastTail) + " " + strings.TrimSpace(tail))
			compacted[len(compacted)-1] = strings.TrimSpace(verb + " " + mergedTail)
		} else {
			mergedTail := DeduplicateInlineCodeTail(lastTail, tail)
			compacted[len(compacted)-1] = strings.TrimSpace(verb + " " + mergedTail)
		}
	}
	return compacted
}

// ParseQuietMergeableLine extracts a verb and tail from a line if it starts with a mergeable verb.
func ParseQuietMergeableLine(line string) (verb, tail string, ok bool) {
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

// DeduplicateInlineCodeTail merges two space-separated lists of backtick-quoted
// items, removing duplicates while preserving order.
func DeduplicateInlineCodeTail(existing, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if existing == "" {
		return incoming
	}
	if incoming == "" {
		return existing
	}
	seen := make(map[string]bool)
	var result []string
	for _, item := range ParseInlineCodeItems(existing) {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	for _, item := range ParseInlineCodeItems(incoming) {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return strings.Join(result, " ")
}

// ParseInlineCodeItems extracts backtick-quoted items from a string.
func ParseInlineCodeItems(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var items []string
	for {
		start := strings.Index(s, "`")
		if start < 0 {
			break
		}
		end := strings.Index(s[start+1:], "`")
		if end < 0 {
			break
		}
		items = append(items, s[start:start+end+2])
		s = s[start+end+2:]
	}
	return items
}

// NormalizeWorkingStatus normalizes a working status value.
func NormalizeWorkingStatus(v any) string {
	status := strings.ToLower(strings.TrimSpace(turnitem.StringValue(v)))
	status = strings.ReplaceAll(status, "_", "")
	return status
}

// NormalizeCommandActionType normalizes a command action type value.
func NormalizeCommandActionType(v any) string {
	raw := strings.ToLower(strings.TrimSpace(turnitem.StringValue(v)))
	raw = strings.ReplaceAll(raw, "_", "")
	raw = strings.ReplaceAll(raw, "-", "")
	return raw
}

// NormalizeWebSearchActionType normalizes a web search action type value.
func NormalizeWebSearchActionType(v any) string {
	raw := strings.ToLower(strings.TrimSpace(turnitem.StringValue(v)))
	raw = strings.ReplaceAll(raw, "_", "")
	raw = strings.ReplaceAll(raw, "-", "")
	return raw
}

// QuietPatchChangeType extracts the change type from a patch change value.
func QuietPatchChangeType(v any) string {
	switch x := v.(type) {
	case map[string]any:
		return strings.ToLower(strings.TrimSpace(apputil.FirstNonEmpty(turnitem.StringValue(x["type"]), turnitem.StringValue(x["kind"]))))
	default:
		return strings.ToLower(strings.TrimSpace(turnitem.StringValue(v)))
	}
}

// QuietPatchMovePath extracts the move path from a patch change value.
func QuietPatchMovePath(v any) string {
	change, _ := v.(map[string]any)
	if change == nil {
		return ""
	}
	return apputil.FirstNonEmpty(turnitem.StringValue(change["move_path"]), turnitem.StringValue(change["movePath"]))
}

// MarkdownInlineCodeSlice wraps each value in inline code, deduplicating first.
func MarkdownInlineCodeSlice(values []string) []string {
	values = QuietDedupeStrings(values)
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if code := turnitem.MarkdownInlineCode(value); code != "" {
			parts = append(parts, code)
		}
	}
	return parts
}

// QuietDedupeStrings removes duplicate and empty strings from the slice.
func QuietDedupeStrings(values []string) []string {
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

// EqualStringSlices reports whether two string slices have identical elements.
func EqualStringSlices(a, b []string) bool {
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

// JoinQuietStringList joins a list value (string or any slice) with " | ".
func JoinQuietStringList(v any) string {
	switch x := v.(type) {
	case []string:
		return strings.Join(turnitem.TrimmedNonEmptyStrings(x), " | ")
	case []any:
		values := make([]string, 0, len(x))
		for _, raw := range x {
			if text := strings.TrimSpace(turnitem.StringValue(raw)); text != "" {
				values = append(values, text)
			}
		}
		return strings.Join(values, " | ")
	default:
		return ""
	}
}
