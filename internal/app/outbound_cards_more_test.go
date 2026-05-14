package app

import (
	"strings"
	"testing"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestRenderMarkdownCardsUsesPlaceholderAndMeta(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg}
	sub := &state.Submission{WorkspaceID: "default"}

	reply := cardRendererForApp(a).renderReplyMarkdownCardWithHeaderOptions(nil, sub, "Reply", "green", true, "", nil, false)
	if got := cardHeaderTitle(t, reply); got != "Reply" {
		t.Fatalf("reply card title = %q, want Reply", got)
	}
	elements := reply["body"].(map[string]any)["elements"].([]map[string]any)
	if len(elements) != 1 || elements[0]["content"] != " " {
		t.Fatalf("reply placeholder elements = %#v, want single blank markdown", elements)
	}
	if body := cardMarkdownContent(t, reply); strings.Contains(body, "当前模式: plan") {
		t.Fatalf("reply placeholder body = %q, want no plan banner", body)
	}

	compact := cardRendererForApp(a).renderCompactMarkdownCard(sub, "Status", "orange", " status=running ", "hello", []feishu.Button{{Text: "More", Type: "default"}})
	if got := cardHeaderTitle(t, compact); got != "Status" {
		t.Fatalf("compact card title = %q, want Status", got)
	}
	body := compact["body"].(map[string]any)["elements"].([]map[string]any)
	if len(body) != 3 {
		t.Fatalf("compact card elements = %#v, want meta + markdown + button row", body)
	}
	if body[0]["tag"] != "div" || body[1]["tag"] != "markdown" || body[2]["tag"] != "column_set" {
		t.Fatalf("compact card layout = %#v, want div/markdown/column_set", body)
	}
	if content := cardMarkdownContent(t, compact); strings.Contains(content, "当前模式: plan") {
		t.Fatalf("compact card content = %q, want no plan banner", content)
	}
}

func TestPlanModeSessionCardsPrefixWorkspaceAndPlan(t *testing.T) {
	a, _, _ := newTestApp(t)
	sessionKey := "feishu:p2p:chat:user"
	workspaceID := a.cfg.Workspaces[0].ID
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             workspaceID,
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: workspaceID,
		ActiveThreadCollaborationMode: &state.SessionCollaborationMode{
			Mode:  "plan",
			Model: "gpt-5.4",
		},
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	cases := []struct {
		name string
		card map[string]any
	}{
		{name: "root", card: renderCommandMenuCard(a, sessionKey)},
		{name: "tools", card: renderToolsMenuCard(a, sessionKey)},
		{name: "status", card: renderStatusCard(a, sessionKey)},
		{name: "quiet", card: renderQuietModeMenuCard(a, sessionKey)},
		{name: "interrupt", card: renderInterruptPreparingCard(a, sessionKey, "menu.tools")},
		{name: "compact", card: newCompactService(a).RenderCompactPreparingCard(sessionKey)},
		{name: "help", card: renderHelpCard(a, sessionKey)},
	}
	for _, tc := range cases {
		if got := cardHeaderTitle(t, tc.card); !strings.HasPrefix(got, "["+workspaceID+"] [plan] ") {
			t.Fatalf("%s card title = %q, want [%s] [plan] prefix", tc.name, got, workspaceID)
		}
		if body := cardMarkdownContent(t, tc.card); strings.Contains(body, "当前模式: plan") {
			t.Fatalf("%s card body = %q, want no plan banner", tc.name, body)
		}
	}
}

func TestPrepareReplyCardMarkdownKeepsPreviewLinksWithLineNumbers(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg}
	sub := &state.Submission{WorkspaceID: "default"}

	body := prepareReplyCardMarkdown(a, nil, sub, "[internal/app/outbound_cards.go:117](https://drive.example/file-1)", true)
	if !strings.Contains(body, "[internal/app/outbound_cards.go:117](https://drive.example/file-1)") {
		t.Fatalf("prepareReplyCardMarkdown(preview link) = %q, want preview link preserved", body)
	}
	if strings.Contains(body, "`internal/app/outbound_cards.go:117`") {
		t.Fatalf("prepareReplyCardMarkdown(preview link) = %q, want no duplicate local-path neutralization", body)
	}
}

func TestPrepareReplyCardMarkdownLinkifiesInlineCodeURLsImmediatelyForPreview(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg}
	sub := &state.Submission{WorkspaceID: "default"}

	body := prepareReplyCardMarkdown(a, nil, sub, "卡片链接：`https://github.com/yuhuan417/feidex`", true)
	if !strings.Contains(body, "[https://github.com/yuhuan417/feidex](https://github.com/yuhuan417/feidex)") {
		t.Fatalf("prepareReplyCardMarkdown(inline-code url) = %q, want markdown link", body)
	}
	if strings.Contains(body, "`https://github.com/yuhuan417/feidex`") {
		t.Fatalf("prepareReplyCardMarkdown(inline-code url) = %q, want no inline-code URL", body)
	}
}
