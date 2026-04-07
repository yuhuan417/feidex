package app

import (
	"strings"
	"testing"

	"feidex/internal/config"
	"feidex/internal/state"
)

func TestRenderSubmissionCardShowsFullContentInCard(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()
	a := &App{cfg: cfg}

	reply := strings.Repeat("hello ", 120)
	command := strings.Repeat("output\n", 80)
	sub := &state.Submission{
		SessionKey:  "sess-1",
		WorkspaceID: "default",
		InputText:   "show me everything",
		CommandText: command,
		OutputText:  reply,
		Status:      "running",
	}

	card := a.renderSubmissionCard(sub, sub.Status)
	elements, ok := card["elements"].([]map[string]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("unexpected card elements: %#v", card["elements"])
	}
	body, ok := elements[0]["content"].(string)
	if !ok {
		t.Fatalf("unexpected card content: %#v", elements[0]["content"])
	}
	if !strings.Contains(body, "输入:\nshow me everything") {
		t.Fatalf("input missing from body: %q", body)
	}
	if !strings.Contains(body, "命令输出:\n"+strings.TrimSpace(command)) {
		t.Fatalf("command output missing from body: %q", body)
	}
	if !strings.Contains(body, "回复:\n"+strings.TrimSpace(reply)) {
		t.Fatalf("reply missing from body: %q", body)
	}
	if strings.Contains(body, "调试:") {
		t.Fatalf("debug block should not be present: %q", body)
	}
}

func TestNormalizeCardMarkdownNormalizesFenceSyntax(t *testing.T) {
	got := normalizeCardMarkdown("```txt\nhello")
	want := "```\nhello\n```"
	if got != want {
		t.Fatalf("unexpected normalized markdown:\nwant: %q\ngot:  %q", want, got)
	}
}
