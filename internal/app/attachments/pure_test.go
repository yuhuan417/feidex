package attachments

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/state"
)

func TestAttachmentHelpers(t *testing.T) {
	workspace := t.TempDir()
	linked := filepath.Join(workspace, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(linked), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(linked, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(linked) error = %v", err)
	}
	repaired := filepath.Join(workspace+".md", "docs", "guide")

	dir := SessionAttachmentDir(workspace, "session", "message")
	if !strings.Contains(dir, AttachmentsDirName) {
		t.Fatalf("SessionAttachmentDir() = %q, want attachments dir", dir)
	}
	if len(ShortHash("value")) != 24 {
		t.Fatalf("ShortHash() length = %d, want 24", len(ShortHash("value")))
	}

	sub := &state.Submission{
		InputText: "hello",
		Skills: []state.SubmissionSkill{
			{Name: "openai-docs", Path: "/skills/openai-docs"},
		},
		Attachments: []state.SubmissionAttachment{
			{Kind: "image", LocalPath: "/tmp/image.png"},
			{Kind: "file", LocalPath: "/tmp/doc.txt"},
			{Kind: "audio", LocalPath: "/tmp/audio.wav"},
		},
	}
	inputs := BuildTurnInputs(sub)
	if len(inputs) != 5 || inputs[0]["type"] != "skill" || inputs[1]["type"] != "text" || inputs[2]["type"] != "localImage" {
		t.Fatalf("BuildTurnInputs() = %+v, want skill + text + localImage + prompts", inputs)
	}
	if inputs[0]["name"] != "openai-docs" || inputs[0]["path"] != "/skills/openai-docs" {
		t.Fatalf("BuildTurnInputs() skill item = %+v, want name/path", inputs[0])
	}
	if got := AttachmentPrompt(state.SubmissionAttachment{Kind: "audio", LocalPath: "/tmp/a.wav"}); !strings.Contains(got, "audio file") {
		t.Fatalf("AttachmentPrompt(audio) = %q, want audio text", got)
	}
	if got := AttachmentPrompt(state.SubmissionAttachment{}); got != "" {
		t.Fatalf("AttachmentPrompt(empty) = %q, want empty", got)
	}

	preview := SubmissionInputPreview(&state.Submission{
		InputText: "Question",
		Skills: []state.SubmissionSkill{
			{Name: "openai-docs", Path: "/skills/openai-docs"},
		},
		Attachments: []state.SubmissionAttachment{
			{Kind: "image", Name: "pic.png"},
			{Kind: "file", LocalPath: "/tmp/report.pdf"},
		},
	})
	if !strings.Contains(preview, "[skill] openai-docs") || !strings.Contains(preview, "Question") || !strings.Contains(preview, "[图片] pic.png") || !strings.Contains(preview, "[文件] report.pdf") {
		t.Fatalf("SubmissionInputPreview() = %q, want skill, text and attachment previews", preview)
	}
	if got := SubmissionInputPreview(&state.Submission{}); got != "-" {
		t.Fatalf("SubmissionInputPreview(empty) = %q, want -", got)
	}

	if path, ok := NormalizeReferencedPath("./docs/guide.md:12", workspace); !ok || path != linked {
		t.Fatalf("NormalizeReferencedPath(relative) = %q, %v, want %q", path, ok, linked)
	}
	if path, ok := NormalizeReferencedPath(repaired, workspace); !ok || path != linked {
		t.Fatalf("NormalizeReferencedPath(repaired) = %q, %v, want %q", path, ok, linked)
	}
	if _, ok := NormalizeReferencedPath("../outside.txt", workspace); ok {
		t.Fatal("expected outside workspace path to be rejected")
	}

	sanitized := SanitizeLocalMarkdownLinks(
		"See [Guide](./docs/guide.md:12) and [.mdguide](missing)",
		workspace,
	)
	if !strings.Contains(sanitized, "`docs/guide.md:12`") {
		t.Fatalf("SanitizeLocalMarkdownLinks() = %q, want workspace-relative local path replacement", sanitized)
	}
	sanitizedMissing := SanitizeLocalMarkdownLinks(
		"See [Missing](./docs/missing.txt:9)",
		workspace,
	)
	if !strings.Contains(sanitizedMissing, "`docs/missing.txt:9`") {
		t.Fatalf("SanitizeLocalMarkdownLinks(missing) = %q, want workspace-relative local path", sanitizedMissing)
	}
	sanitizedAbsolute := SanitizeLocalMarkdownLinks(
		"See [Guide]("+linked+":12)",
		workspace,
	)
	if !strings.Contains(sanitizedAbsolute, "`docs/guide.md:12`") {
		t.Fatalf("SanitizeLocalMarkdownLinks(abs) = %q, want workspace-relative absolute path", sanitizedAbsolute)
	}
	neutralized := NeutralizeLocalMarkdownLinks(
		"See [Guide](./docs/guide.md:12) and [Web](https://example.com)",
		workspace,
	)
	if !strings.Contains(neutralized, "`docs/guide.md:12`") || !strings.Contains(neutralized, "[Web](https://example.com)") {
		t.Fatalf("NeutralizeLocalMarkdownLinks() = %q, want local de-link + remote keep", neutralized)
	}
	if _, ok := LocalLinkDisplayTarget("https://example.com/x.md", workspace); ok {
		t.Fatal("LocalLinkDisplayTarget(https) should be non-local")
	}
	if got, ok := LocalLinkDisplayTarget("./docs/missing.txt:9", workspace); !ok || got != filepath.Join("docs", "missing.txt")+":9" {
		t.Fatalf("LocalLinkDisplayTarget(missing) = %q, %v, want workspace-relative path", got, ok)
	}
	if got, ok := LocalLinkDisplayTarget(linked+":7", workspace); !ok || got != filepath.Join("docs", "guide.md")+":7" {
		t.Fatalf("LocalLinkDisplayTarget(abs) = %q, %v, want workspace-relative absolute path", got, ok)
	}
	if got := RecoverFilenameFromMalformedLabel(".mdguide"); got != "dguide.m" {
		t.Fatalf("RecoverFilenameFromMalformedLabel() = %q, want dguide.m", got)
	}
	if !IsAlphaNum("abc123") || IsAlphaNum("bad-name") {
		t.Fatal("IsAlphaNum() returned unexpected result")
	}
	if !IsFileNameLike("file_name-1.txt") || IsFileNameLike("bad/name") {
		t.Fatal("IsFileNameLike() returned unexpected result")
	}
	if got := TrimLineReferenceSuffix("/tmp/file.go:10:2"); got != "/tmp/file.go" {
		t.Fatalf("TrimLineReferenceSuffix() = %q, want file path", got)
	}
	if !PathWithinWorkspace(linked, workspace) || PathWithinWorkspace(filepath.Join(workspace, "..", "elsewhere"), workspace) {
		t.Fatal("PathWithinWorkspace() returned unexpected result")
	}
}
