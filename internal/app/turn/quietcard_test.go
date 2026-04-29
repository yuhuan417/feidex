package turn

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildWorkingCardLinesFormatsSupportedItems(t *testing.T) {
	workspace := t.TempDir()
	commandItem := map[string]any{
		"type":   "commandExecution",
		"status": "completed",
		"cwd":    workspace,
		"commandActions": []any{
			map[string]any{
				"type": "read",
				"path": filepath.Join(workspace, "internal", "app", "turn_stream.go"),
			},
			map[string]any{
				"type": "read",
				"name": "quiet_mode.go",
			},
			map[string]any{
				"type": "listFiles",
				"path": filepath.Join(workspace, "internal", "app"),
			},
			map[string]any{
				"type":  "search",
				"query": "quiet on",
				"path":  filepath.Join(workspace, "internal", "app"),
			},
			map[string]any{
				"type": "search",
				"path": filepath.Join(workspace, "..", "elsewhere"),
			},
		},
	}

	_, commandLines := BuildWorkingCardLines("item-cmd", commandItem, workspace)
	joinedCommand := strings.Join(commandLines, "\n")
	for _, want := range []string{
		"Read `turn_stream.go` `quiet_mode.go`",
		"List `internal/app`",
		"Search `quiet on` in `internal/app`",
		"Search in `" + filepath.Clean(filepath.Join(workspace, "..", "elsewhere")) + "`",
	} {
		if !strings.Contains(joinedCommand, want) {
			t.Fatalf("command lines missing %q: %q", want, joinedCommand)
		}
	}

	fileItem := map[string]any{
		"type":   "fileChange",
		"status": "completed",
		"changes": []any{
			map[string]any{
				"path": filepath.Join(workspace, "a.txt"),
				"kind": map[string]any{"type": "add"},
			},
			map[string]any{
				"path": filepath.Join(workspace, "b.txt"),
				"kind": map[string]any{"type": "delete"},
			},
			map[string]any{
				"path": filepath.Join(workspace, "c.txt"),
				"kind": map[string]any{"type": "update", "move_path": filepath.Join(workspace, "d.txt")},
			},
		},
	}

	_, fileLines := BuildWorkingCardLines("item-file", fileItem, workspace)
	got := strings.Join(fileLines, "\n")
	if strings.Contains(got, "文件修改中：") {
		t.Fatalf("file lines should not include legacy prefix: %q", got)
	}
	for _, want := range []string{
		"Add `a.txt`",
		"Delete `b.txt`",
		"Update `c.txt` `d.txt`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("file lines missing %q: %q", want, got)
		}
	}

	webSearchItem := map[string]any{
		"type":  "webSearch",
		"query": "golang tips",
		"action": map[string]any{
			"type": "search",
		},
	}
	_, webLines := BuildWorkingCardLines("item-web", webSearchItem, workspace)
	if len(webLines) != 1 || webLines[0] != "Searching the web: `golang tips`" {
		t.Fatalf("web search lines = %#v", webLines)
	}
}

func TestQuietWorkingCardHelperBranches(t *testing.T) {
	if got := BuildQuietWebSearchLines(map[string]any{
		"action": map[string]any{
			"type": "findInPage",
			"url":  " https://example.test/page ",
		},
	}); len(got) != 1 || got[0] != "Find in page: `https://example.test/page`" {
		t.Fatalf("BuildQuietWebSearchLines(findInPage) = %#v", got)
	}

	if got := BuildQuietWebSearchLines(map[string]any{
		"action": map[string]any{
			"queries": []any{" alpha ", "", "beta"},
		},
	}); len(got) != 1 || got[0] != "Searching the web: `alpha | beta`" {
		t.Fatalf("BuildQuietWebSearchLines(queries) = %#v", got)
	}

	if got := BuildQuietWebSearchLines(map[string]any{
		"action": map[string]any{"type": "openPage"},
	}); got != nil {
		t.Fatalf("BuildQuietWebSearchLines(openPage) = %#v, want nil", got)
	}

	if got := BuildQuietCommandExecutionLines(map[string]any{
		"status": "in_progress",
		"commandActions": []any{
			map[string]any{"type": "read", "name": "quiet_mode.go"},
		},
	}, ""); got != nil {
		t.Fatalf("BuildQuietCommandExecutionLines(non-completed) = %#v, want nil", got)
	}

	if got := JoinQuietStringList([]string{" a ", "", "b "}); got != "a | b" {
		t.Fatalf("JoinQuietStringList([]string) = %q", got)
	}
	if got := JoinQuietStringList([]any{" a ", "", "b "}); got != "a | b" {
		t.Fatalf("JoinQuietStringList([]any) = %q", got)
	}

	if got := NormalizeWorkingStatus(" in_progress "); got != "inprogress" {
		t.Fatalf("NormalizeWorkingStatus() = %q", got)
	}
	if got := NormalizeCommandActionType("list-files"); got != "listfiles" {
		t.Fatalf("NormalizeCommandActionType() = %q", got)
	}
	if got := NormalizeWebSearchActionType("find_in_page"); got != "findinpage" {
		t.Fatalf("NormalizeWebSearchActionType() = %q", got)
	}
	if got := QuietPatchChangeType(map[string]any{"kind": "Update"}); got != "update" {
		t.Fatalf("QuietPatchChangeType() = %q", got)
	}
	if got := QuietPatchMovePath(map[string]any{"movePath": "next.go"}); got != "next.go" {
		t.Fatalf("QuietPatchMovePath() = %q", got)
	}

	verb, tail, ok := ParseQuietMergeableLine("Read `a.go`")
	if !ok || verb != "Read" || tail != "`a.go`" {
		t.Fatalf("ParseQuietMergeableLine(Read) = %q %q %v", verb, tail, ok)
	}
	if _, _, ok := ParseQuietMergeableLine("Search `foo`"); ok {
		t.Fatal("ParseQuietMergeableLine(Search) should not be mergeable")
	}

	card := &QuietWorkingCard{}
	if !card.ReplaceEntries(QuietWorkingReasoningKey, []string{"思考中..."}) {
		t.Fatal("ReplaceEntries(reasoning) should report change")
	}
	if !card.IsReasoningOnly() {
		t.Fatal("card should be reasoning-only")
	}
	if got := card.LinesForPrefix(QuietWorkingReasoningKey); len(got) != 1 || got[0] != "思考中..." {
		t.Fatalf("LinesForPrefix(reasoning) = %#v", got)
	}
	if !card.ReplaceEntries("item:1", []string{"Read `a.go`", "Read `b.go`", "List `internal/app`"}) {
		t.Fatal("ReplaceEntries(item) should report change")
	}
	body := card.Body()
	if !strings.Contains(body, "Read `a.go` `b.go`") || !strings.Contains(body, "List `internal/app`") {
		t.Fatalf("Body() = %q", body)
	}
	if !card.RemoveEntries(QuietWorkingReasoningKey) {
		t.Fatal("RemoveEntries(reasoning) should report change")
	}
	if card.IsReasoningOnly() {
		t.Fatal("card should no longer be reasoning-only")
	}
	if card.RemoveEntries("missing") {
		t.Fatal("RemoveEntries(missing) should not report change")
	}
	if prefix := EntryPrefix(EntryKey("item:2", 3)); prefix != "item:2" {
		t.Fatalf("EntryPrefix() = %q", prefix)
	}
	if EqualStringSlices([]string{"a"}, []string{"b"}) || !EqualStringSlices([]string{"a"}, []string{"a"}) {
		t.Fatal("EqualStringSlices() returned unexpected result")
	}
}

func TestCompactQuietWorkingLinesDeduplicates(t *testing.T) {
	lines := CompactQuietWorkingLines([]string{
		"Read `a.go`",
		"Read `a.go`",
		"Read `b.go`",
	})
	if len(lines) != 1 || lines[0] != "Read `a.go` `b.go`" {
		t.Fatalf("dedup same file: lines = %#v", lines)
	}

	lines = CompactQuietWorkingLines([]string{
		"Update `x.go`",
		"Update `x.go`",
		"Update `y.go`",
	})
	if len(lines) != 1 || lines[0] != "Update `x.go` `y.go`" {
		t.Fatalf("dedup update: lines = %#v", lines)
	}
}

func TestCompactQuietWorkingLinesDedupWithKeys(t *testing.T) {
	lines := CompactQuietWorkingLinesWithDedup(
		[]string{"Read `a.go`", "Read `a.go`"},
		[]string{"src/a.go", "src/a.go"},
	)
	if len(lines) != 1 || lines[0] != "Read `a.go`" {
		t.Fatalf("same dedup key: lines = %#v", lines)
	}

	lines = CompactQuietWorkingLinesWithDedup(
		[]string{"Read `a.go`", "Read `a.go`"},
		[]string{"src/a.go", "test/a.go"},
	)
	if len(lines) != 1 || lines[0] != "Read `a.go` `a.go`" {
		t.Fatalf("diff dedup key same name: lines = %#v", lines)
	}

	lines = CompactQuietWorkingLinesWithDedup(
		[]string{"Read `a.go`", "Read `a.go`", "Read `b.go`", "Read `a.go`"},
		[]string{"src/a.go", "src/a.go", "src/b.go", "test/a.go"},
	)
	if len(lines) != 1 || lines[0] != "Read `a.go` `b.go` `a.go`" {
		t.Fatalf("mixed dedup: lines = %#v", lines)
	}
}

func TestQuietWorkingCardBodyDedupKeys(t *testing.T) {
	card := &QuietWorkingCard{
		Entries: map[string]string{
			"item:t1\x000": "Read `a.go`",
			"item:t1\x001": "Read `a.go`",
			"item:t1\x002": "Read `b.go`",
		},
		EntryOrder: []string{"item:t1\x000", "item:t1\x001", "item:t1\x002"},
		DedupKeys: map[string]string{
			"item:t1\x000": "src/a.go",
			"item:t1\x001": "src/a.go",
			"item:t1\x002": "src/b.go",
		},
	}
	body := card.Body()
	if body != "Read `a.go` `b.go`" {
		t.Fatalf("body with dedup keys = %q", body)
	}

	card2 := &QuietWorkingCard{
		Entries: map[string]string{
			"item:t2\x000": "Read `a.go`",
			"item:t2\x001": "Read `a.go`",
		},
		EntryOrder: []string{"item:t2\x000", "item:t2\x001"},
		DedupKeys: map[string]string{
			"item:t2\x000": "src/a.go",
			"item:t2\x001": "test/a.go",
		},
	}
	body2 := card2.Body()
	if body2 != "Read `a.go` `a.go`" {
		t.Fatalf("body same name diff key = %q", body2)
	}
}

func TestPrepareUpdateLockedAndBoundaryLocked(t *testing.T) {
	workspace := t.TempDir()

	if got := PrepareUpdateLocked(nil, "noop", map[string]any{"type": "reasoning"}, workspace); got != (QuietWorkingCardOp{}) {
		t.Fatalf("PrepareUpdateLocked(nil) = %+v", got)
	}

	stream := &StreamState{TurnID: "turn-1"}
	if got := PrepareUpdateLocked(stream, "noop", map[string]any{
		"type": "webSearch",
		"action": map[string]any{
			"type": "openPage",
		},
	}, workspace); got != (QuietWorkingCardOp{}) {
		t.Fatalf("PrepareUpdateLocked(no visible lines) = %+v", got)
	}

	reasoningKey := EntryKey(QuietWorkingReasoningKey, 0)
	stream.QuietWorking = &QuietWorkingCard{
		MessageID:    "msg-work",
		EntryOrder:   []string{reasoningKey},
		Entries:      map[string]string{reasoningKey: "思考中..."},
		RenderedBody: "思考中...",
	}
	op := PrepareUpdateLocked(stream, "cmd-1", map[string]any{
		"type":   "commandExecution",
		"status": "completed",
		"commandActions": []any{
			map[string]any{"type": "read", "name": "quiet_mode.go"},
		},
	}, workspace)
	if op.MessageID != "msg-work" || !strings.Contains(op.Body, "Read `quiet_mode.go`") || strings.Contains(op.Body, "思考中...") {
		t.Fatalf("PrepareUpdateLocked(command) = %+v", op)
	}
	if got := PrepareUpdateLocked(stream, "cmd-1", map[string]any{
		"type":   "commandExecution",
		"status": "completed",
		"commandActions": []any{
			map[string]any{"type": "read", "name": "quiet_mode.go"},
		},
	}, workspace); got != (QuietWorkingCardOp{}) {
		t.Fatalf("PrepareUpdateLocked(unchanged) = %+v", got)
	}

	reasoningOnly := &StreamState{
		QuietWorking: &QuietWorkingCard{
			MessageID:  "reuse-1",
			EntryOrder: []string{reasoningKey},
			Entries:    map[string]string{reasoningKey: "思考中..."},
		},
	}
	boundary := PrepareBoundaryLocked(reasoningOnly)
	if boundary.ReuseMessageID != "reuse-1" || boundary.Op != (QuietWorkingCardOp{}) || reasoningOnly.QuietWorking != nil {
		t.Fatalf("PrepareBoundaryLocked(reasoning-only) = %+v, stream=%+v", boundary, reasoningOnly)
	}

	itemKey := EntryKey("item:cmd-1", 0)
	mixed := &StreamState{
		TurnID: "turn-2",
		QuietWorking: &QuietWorkingCard{
			MessageID:    "patch-1",
			RenderedBody: "思考中...\nRead `quiet_mode.go`",
			EntryOrder:   []string{reasoningKey, itemKey},
			Entries: map[string]string{
				reasoningKey: "思考中...",
				itemKey:      "Read `quiet_mode.go`",
			},
		},
	}
	boundary = PrepareBoundaryLocked(mixed)
	if boundary.ReuseMessageID != "" || boundary.Op.MessageID != "patch-1" || boundary.Op.Body != "Read `quiet_mode.go`" || mixed.QuietWorking != nil {
		t.Fatalf("PrepareBoundaryLocked(mixed) = %+v, stream=%+v", boundary, mixed)
	}
}

func TestCompactQuietWorkingLinesMergesAdjacentSameVerb(t *testing.T) {
	lines := CompactQuietWorkingLines([]string{
		"Read `a.go`",
		"Read `b.go`",
		"List `internal/app`",
		"List `internal/state`",
		"Search `quiet` in `internal/app`",
		"Add `x.go`",
		"Add `y.go`",
		"Delete `z.go`",
	})
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"Read `a.go` `b.go`",
		"List `internal/app` `internal/state`",
		"Search `quiet` in `internal/app`",
		"Add `x.go` `y.go`",
		"Delete `z.go`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compacted lines missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "Read `a.go`\nRead `b.go`") || strings.Contains(got, "Add `x.go`\nAdd `y.go`") {
		t.Fatalf("compacted lines should merge adjacent same verbs: %q", got)
	}
}
