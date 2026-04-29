package linkutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	got := NormalizeLocalFilePreviewTargets(strings.Join([]string{
		"See `docs/guide.md:12` for details.",
		"[keep](https://example.com/guide)",
		"```md",
		"`docs/guide.md:99` should stay untouched inside fences.",
		"```",
	}, "\n"), workspace)

	if !strings.Contains(got, "See [docs/guide.md:12](docs/guide.md:12) for details.") {
		t.Fatalf("NormalizeLocalFilePreviewTargets() = %q, want inline code file ref to become markdown link", got)
	}
	if !strings.Contains(got, "[keep](https://example.com/guide)") {
		t.Fatalf("NormalizeLocalFilePreviewTargets() = %q, want remote markdown link preserved", got)
	}
	if !strings.Contains(got, "`docs/guide.md:99` should stay untouched inside fences.") {
		t.Fatalf("NormalizeLocalFilePreviewTargets() = %q, want fenced code content preserved", got)
	}
}
