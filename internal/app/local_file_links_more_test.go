package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestNormalizeLocalFilePreviewTargetsLinkifiesInlineCodeRefs(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("MkdirAll(target dir) error = %v", err)
	}
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}

	got := normalizeLocalFilePreviewTargets(strings.Join([]string{
		"See `docs/guide.md:12` for details.",
		"[keep](https://example.com/guide)",
		"```md",
		"`docs/guide.md:99` should stay untouched inside fences.",
		"```",
	}, "\n"), workspace)

	if !strings.Contains(got, "See [docs/guide.md:12](docs/guide.md:12) for details.") {
		t.Fatalf("normalizeLocalFilePreviewTargets() = %q, want inline code file ref to become markdown link", got)
	}
	if !strings.Contains(got, "[keep](https://example.com/guide)") {
		t.Fatalf("normalizeLocalFilePreviewTargets() = %q, want remote markdown link preserved", got)
	}
	if !strings.Contains(got, "`docs/guide.md:99` should stay untouched inside fences.") {
		t.Fatalf("normalizeLocalFilePreviewTargets() = %q, want fenced code content preserved", got)
	}
}

func TestRewriteLocalFileLinksTextNormalizesInlineCodeRefsForPreview(t *testing.T) {
	a, ff, _ := newTestApp(t)
	workspace := a.cfg.Workspaces[0].Cwd
	target := filepath.Join(workspace, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("MkdirAll(target dir) error = %v", err)
	}
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}

	ff.rewriteLocalFileLinksHook = func(_ context.Context, req feishu.LocalFileLinkRewriteRequest) (string, error) {
		const raw = "[docs/guide.md:12](docs/guide.md:12)"
		const preview = "[docs/guide.md:12](https://drive.example/file-1)"
		if !strings.Contains(req.Text, raw) {
			return req.Text, nil
		}
		return strings.ReplaceAll(req.Text, raw, preview), nil
	}

	sub := &state.Submission{
		ID:          "sub-1",
		WorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:      "chat-1",
		UserID:      "user-1",
	}
	got := rewriteLocalFileLinksText(a, context.Background(), sub, "See `docs/guide.md:12` for details.")

	if len(ff.rewriteLocalFileLinkReqs) != 1 {
		t.Fatalf("RewriteLocalFileLinks request count = %d, want 1", len(ff.rewriteLocalFileLinkReqs))
	}
	if req := ff.rewriteLocalFileLinkReqs[0]; !strings.Contains(req.Text, "[docs/guide.md:12](docs/guide.md:12)") {
		t.Fatalf("RewriteLocalFileLinks request text = %q, want normalized markdown link", req.Text)
	}
	if !strings.Contains(got, "[docs/guide.md:12](https://drive.example/file-1)") {
		t.Fatalf("rewriteLocalFileLinksText() = %q, want preview link", got)
	}
}
