package submission

import (
	"encoding/json"
	"testing"
	"time"

	"feidex/internal/state"
)

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect []string
	}{
		{"nil", nil, nil},
		{"empty", []string{}, []string{}},
		{"dedup", []string{"a", "b", "a", "c"}, []string{"a", "b", "c"}},
		{"trims whitespace", []string{" a ", "b", " a"}, []string{"a", "b"}},
		{"skips empty", []string{"", "a", " ", "b"}, []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UniqueStrings(tt.input)
			if len(got) != len(tt.expect) {
				t.Fatalf("UniqueStrings(%v) = %v, want %v", tt.input, got, tt.expect)
			}
			for i := range got {
				if got[i] != tt.expect[i] {
					t.Fatalf("UniqueStrings(%v)[%d] = %q, want %q", tt.input, i, got[i], tt.expect[i])
				}
			}
		})
	}
}

func TestRemoveString(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		target string
		expect []string
	}{
		{"remove first", []string{"a", "b", "a"}, "a", []string{"b", "a"}},
		{"not found", []string{"a", "b"}, "c", []string{"a", "b"}},
		{"empty target", []string{"a", "b"}, "", []string{"a", "b"}},
		{"trims target", []string{"a", "b"}, " a ", []string{"b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RemoveString(tt.values, tt.target)
			if len(got) != len(tt.expect) {
				t.Fatalf("RemoveString(%v, %q) = %v, want %v", tt.values, tt.target, got, tt.expect)
			}
			for i := range got {
				if got[i] != tt.expect[i] {
					t.Fatalf("RemoveString(%v, %q)[%d] = %q, want %q", tt.values, tt.target, i, got[i], tt.expect[i])
				}
			}
		})
	}
}

func TestStagedImageAttachments(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if got := StagedImageAttachments(nil); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
	t.Run("skips empty local path", func(t *testing.T) {
		images := []state.SessionStagedImage{
			{Name: "a", LocalPath: "/path/a"},
			{Name: "b", LocalPath: "  "},
			{Name: "c", LocalPath: "/path/c"},
		}
		got := StagedImageAttachments(images)
		if len(got) != 2 {
			t.Fatalf("expected 2 attachments, got %d", len(got))
		}
		if got[0].Kind != "image" || got[0].LocalPath != "/path/a" {
			t.Fatalf("unexpected first attachment: %+v", got[0])
		}
	})
}

func TestStagedImageSourceMessageIDs(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if got := StagedImageSourceMessageIDs(nil); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
	t.Run("deduplicates", func(t *testing.T) {
		images := []state.SessionStagedImage{
			{SourceMessageID: "msg-1"},
			{SourceMessageID: "msg-2"},
			{SourceMessageID: "msg-1"},
		}
		got := StagedImageSourceMessageIDs(images)
		if len(got) != 2 || got[0] != "msg-1" || got[1] != "msg-2" {
			t.Fatalf("unexpected result: %v", got)
		}
	})
}

func TestStagedImageRootMessageIDs(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if got := StagedImageRootMessageIDs(nil); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
	t.Run("falls back to source message ID", func(t *testing.T) {
		images := []state.SessionStagedImage{
			{RootMessageID: "root-1", SourceMessageID: "src-1"},
			{RootMessageID: "", SourceMessageID: "src-2"},
		}
		got := StagedImageRootMessageIDs(images)
		if len(got) != 2 || got[0] != "root-1" || got[1] != "src-2" {
			t.Fatalf("unexpected result: %v", got)
		}
	})
}

func TestHasSourceMessage(t *testing.T) {
	t.Run("nil submission", func(t *testing.T) {
		if HasSourceMessage(nil, "msg-1") {
			t.Fatal("expected false for nil submission")
		}
	})
	t.Run("matches trigger message", func(t *testing.T) {
		sub := &state.Submission{TriggerMessageID: "msg-1"}
		if !HasSourceMessage(sub, "msg-1") {
			t.Fatal("expected true for trigger message match")
		}
	})
	t.Run("matches source message", func(t *testing.T) {
		sub := &state.Submission{SourceMessageIDs: []string{"msg-2", "msg-3"}}
		if !HasSourceMessage(sub, "msg-2") {
			t.Fatal("expected true for source message match")
		}
	})
	t.Run("no match", func(t *testing.T) {
		sub := &state.Submission{TriggerMessageID: "msg-1"}
		if HasSourceMessage(sub, "msg-99") {
			t.Fatal("expected false for no match")
		}
	})
}

func TestSourceMessageIDs(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if got := SourceMessageIDs(nil); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
	t.Run("includes trigger", func(t *testing.T) {
		sub := &state.Submission{
			SourceMessageIDs: []string{"msg-1"},
			TriggerMessageID: "msg-2",
		}
		got := SourceMessageIDs(sub)
		if len(got) != 2 {
			t.Fatalf("expected 2 IDs, got %d: %v", len(got), got)
		}
	})
	t.Run("deduplicates trigger", func(t *testing.T) {
		sub := &state.Submission{
			SourceMessageIDs: []string{"msg-1"},
			TriggerMessageID: "msg-1",
		}
		got := SourceMessageIDs(sub)
		if len(got) != 1 {
			t.Fatalf("expected 1 ID, got %d: %v", len(got), got)
		}
	})
}

func TestCompletionTerminalText(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		lastError string
		expect    string
	}{
		{"completed returns empty", "completed", "", ""},
		{"completed with error still empty", "completed", "some error", ""},
		{"interrupted always returns message", "interrupted", "", "任务已中断。"},
		{"interrupted ignores error", "interrupted", "some error", "任务已中断。"},
		{"failed with error uses error", "failed", "connection refused", "connection refused"},
		{"failed without error uses default", "failed", "", "任务失败。"},
		{"unknown status uses default", "unknown", "", "任务已结束。"},
		{"unknown status with error uses error", "unknown", "oops", "oops"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompletionTerminalText(tt.status, tt.lastError)
			if got != tt.expect {
				t.Fatalf("CompletionTerminalText(%q, %q) = %q, want %q", tt.status, tt.lastError, got, tt.expect)
			}
		})
	}
}

func TestPendingConfirmationText(t *testing.T) {
	got := PendingConfirmationText(" mySkill ")
	if got != "已识别到技能 `mySkill`，请确认是否使用。" {
		t.Fatalf("unexpected result: %q", got)
	}
}

func TestRefreshPendingStatus(t *testing.T) {
	t.Run("nil session", func(t *testing.T) {
		RefreshPendingStatus(nil) // should not panic
	})
	t.Run("queued when queue has items", func(t *testing.T) {
		sess := &state.Session{Queue: []string{"sub-1"}}
		RefreshPendingStatus(sess)
		if sess.Status != state.SessionStatusQueued.String() {
			t.Fatalf("expected queued, got %q", sess.Status)
		}
	})
	t.Run("queued when staged images", func(t *testing.T) {
		sess := &state.Session{StagedImages: []state.SessionStagedImage{{SourceMessageID: "img-1"}}}
		RefreshPendingStatus(sess)
		if sess.Status != state.SessionStatusQueued.String() {
			t.Fatalf("expected queued, got %q", sess.Status)
		}
	})
	t.Run("idle when empty", func(t *testing.T) {
		sess := &state.Session{Status: state.SessionStatusQueued.String()}
		RefreshPendingStatus(sess)
		if sess.Status != state.SessionStatusIdle.String() {
			t.Fatalf("expected idle, got %q", sess.Status)
		}
	})
	t.Run("no change when in flight", func(t *testing.T) {
		sess := &state.Session{
			ActiveSubmissionID: "sub-running",
			Status:             "turn_in_progress",
			Queue:              []string{"sub-1"},
		}
		RefreshPendingStatus(sess)
		if sess.Status != "turn_in_progress" {
			t.Fatalf("expected status unchanged, got %q", sess.Status)
		}
	})
}

func TestDiscardStagedImageByMessageID(t *testing.T) {
	t.Run("nil session", func(t *testing.T) {
		if DiscardStagedImageByMessageID(nil, "msg-1") {
			t.Fatal("expected false for nil session")
		}
	})
	t.Run("discards matching image", func(t *testing.T) {
		sess := &state.Session{
			StagedImages: []state.SessionStagedImage{
				{SourceMessageID: "msg-1", LocalPath: "/a"},
				{SourceMessageID: "msg-2", LocalPath: "/b"},
			},
		}
		if !DiscardStagedImageByMessageID(sess, "msg-1") {
			t.Fatal("expected true")
		}
		if len(sess.StagedImages) != 1 || sess.StagedImages[0].SourceMessageID != "msg-2" {
			t.Fatalf("unexpected remaining images: %+v", sess.StagedImages)
		}
	})
	t.Run("no match returns false", func(t *testing.T) {
		sess := &state.Session{
			StagedImages: []state.SessionStagedImage{
				{SourceMessageID: "msg-1"},
			},
		}
		if DiscardStagedImageByMessageID(sess, "msg-99") {
			t.Fatal("expected false")
		}
	})
}

func TestPendingTextRequest(t *testing.T) {
	now := time.Now().UnixMilli()
	allPending := []*state.PendingRequest{
		{ID: "old", SessionKey: "sess-1", OwnerUserID: "user-1", Kind: "approval", Status: "pending", CreatedAt: now - 1000},
		{ID: "new", SessionKey: "sess-1", OwnerUserID: "user-1", Kind: "approval", Status: "pending", CreatedAt: now},
		{ID: "other-sess", SessionKey: "sess-2", OwnerUserID: "user-1", Kind: "approval", Status: "pending", CreatedAt: now},
	}
	kinds := map[string]bool{"approval": true}

	t.Run("returns most recent", func(t *testing.T) {
		got := PendingTextRequest(allPending, "sess-1", "user-1", kinds)
		if got == nil || got.ID != "new" {
			t.Fatalf("expected 'new', got %+v", got)
		}
	})
	t.Run("returns nil for wrong session", func(t *testing.T) {
		got := PendingTextRequest(allPending, "sess-99", "user-1", kinds)
		if got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})
	t.Run("matches any user when owner is empty", func(t *testing.T) {
		pending := []*state.PendingRequest{
			{ID: "no-owner", SessionKey: "sess-1", OwnerUserID: "", Kind: "approval", Status: "pending", CreatedAt: now},
		}
		got := PendingTextRequest(pending, "sess-1", "anyone", kinds)
		if got == nil || got.ID != "no-owner" {
			t.Fatalf("expected 'no-owner', got %+v", got)
		}
	})
}

func TestShouldRedactInboundText(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if ShouldRedactInboundText(nil) {
			t.Fatal("expected false for nil")
		}
	})
	t.Run("mcp_elicitation_form always redacts", func(t *testing.T) {
		if !ShouldRedactInboundText(&state.PendingRequest{Kind: "mcp_elicitation_form"}) {
			t.Fatal("expected true")
		}
	})
	t.Run("form with secret question redacts", func(t *testing.T) {
		payload := ToolUserInputPayload{
			Questions: []ToolUserInputQuestion{
				{Question: "API key", IsSecret: true},
			},
		}
		b, _ := json.Marshal(payload)
		if !ShouldRedactInboundText(&state.PendingRequest{
			Kind:        "tool_request_user_input_form",
			PayloadJSON: string(b),
		}) {
			t.Fatal("expected true for secret question")
		}
	})
	t.Run("form without secrets does not redact", func(t *testing.T) {
		payload := ToolUserInputPayload{
			Questions: []ToolUserInputQuestion{
				{Question: "name", IsSecret: false},
			},
		}
		b, _ := json.Marshal(payload)
		if ShouldRedactInboundText(&state.PendingRequest{
			Kind:        "tool_request_user_input_form",
			PayloadJSON: string(b),
		}) {
			t.Fatal("expected false for non-secret question")
		}
	})
	t.Run("unknown kind does not redact", func(t *testing.T) {
		if ShouldRedactInboundText(&state.PendingRequest{Kind: "approval"}) {
			t.Fatal("expected false for unknown kind")
		}
	})
}

func TestCancelledPendingTitle(t *testing.T) {
	tests := []struct {
		name     string
		pending  *state.PendingRequest
		planMode string
		review   string
		expect   string
	}{
		{"nil pending", nil, "plan", "review", "已取消"},
		{"plan mode", &state.PendingRequest{Kind: "plan"}, "plan", "review", "计划确认已取消"},
		{"user input form", &state.PendingRequest{Kind: "tool_request_user_input_form"}, "plan", "review", "输入请求已取消"},
		{"elicitation form", &state.PendingRequest{Kind: "mcp_elicitation_form"}, "plan", "review", "表单请求已取消"},
		{"review", &state.PendingRequest{Kind: "review"}, "plan", "review", "Review 已取消"},
		{"unknown kind", &state.PendingRequest{Kind: "other"}, "plan", "review", "已取消"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CancelledPendingTitle(tt.pending, tt.planMode, tt.review)
			if got != tt.expect {
				t.Fatalf("CancelledPendingTitle() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestParseStructuredLines(t *testing.T) {
	got := ParseStructuredLines("name: Alice\nage: 30\ninvalid line")
	if got["name"] != "Alice" || got["age"] != "30" {
		t.Fatalf("unexpected result: %v", got)
	}
	if _, ok := got["invalid line"]; ok {
		t.Fatal("expected invalid line to be skipped")
	}
}

func TestShouldStartNextSubmissionAsync(t *testing.T) {
	tests := []struct {
		name string
		sess *state.Session
		want bool
	}{
		{"nil session", nil, false},
		{"empty session", &state.Session{}, false},
		{"staged images only", &state.Session{
			StagedImages: []state.SessionStagedImage{{SourceMessageID: "img-1"}},
		}, false},
		{"queued submission only", &state.Session{
			Queue: []string{"sub-1"},
		}, true},
		{"active submission blocks", &state.Session{
			Queue:              []string{"sub-1"},
			ActiveSubmissionID: "sub-running",
		}, false},
		{"active operations block", &state.Session{
			Queue: []string{"sub-1"},
			ActiveOperations: []state.SessionActiveOperation{{
				Kind:         "submission",
				SubmissionID: "sub-running",
				TurnID:       "turn-1",
			}},
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldStartNextSubmissionAsync(tt.sess); got != tt.want {
				t.Fatalf("ShouldStartNextSubmissionAsync() = %v, want %v", got, tt.want)
			}
		})
	}
}
