package pathdisplay

import (
	"path/filepath"
	"testing"
)

func TestRenderWorkspaceDisplayPath(t *testing.T) {
	workspace := t.TempDir()
	linked := filepath.Join(workspace, "docs", "guide.md")

	if got := RenderWorkspaceDisplayPath(linked+":12", workspace); got != filepath.Join("docs", "guide.md")+":12" {
		t.Fatalf("RenderWorkspaceDisplayPath(internal) = %q", got)
	}
	if got := RenderWorkspaceDisplayPath(linked+"#L12", workspace); got != filepath.Join("docs", "guide.md")+":12" {
		t.Fatalf("RenderWorkspaceDisplayPath(line anchor) = %q", got)
	}
	if got := RenderWorkspaceDisplayPath(linked+"#L12C3", workspace); got != filepath.Join("docs", "guide.md")+":12:3" {
		t.Fatalf("RenderWorkspaceDisplayPath(line+column anchor) = %q", got)
	}
	if got := RenderWorkspaceDisplayPath(filepath.Join(workspace, "..", "elsewhere", "x.go"), workspace); !filepath.IsAbs(got) {
		t.Fatalf("RenderWorkspaceDisplayPath(external) = %q, want absolute path", got)
	}
}
