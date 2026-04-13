package feishu

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

func TestMarkdownPreviewHelpers(t *testing.T) {
	if got := (&driveAPIError{Code: 403, Msg: " denied "}).Error(); got != "feishu drive api error 403: denied" {
		t.Fatalf("driveAPIError.Error() = %q", got)
	}

	a := &Adapter{}
	text, err := a.RewriteMarkdownPreview(context.Background(), MarkdownPreviewRequest{Text: "hello"})
	if err != nil || text != "hello" {
		t.Fatalf("RewriteMarkdownPreview(nil previewer) = %q, %v", text, err)
	}
	if _, err := a.CleanupMarkdownPreviewsBefore(context.Background(), time.Now()); err != nil {
		t.Fatalf("CleanupMarkdownPreviewsBefore(nil previewer) error = %v", err)
	}

	a = &Adapter{client: lark.NewClient("app", "secret"), previewProcessCWD: "/repo"}
	previewer := a.ensureMarkdownPreviewer()
	if previewer == nil || previewer.config.ProcessCWD != "/repo" {
		t.Fatalf("ensureMarkdownPreviewer() = %+v, want configured previewer", previewer)
	}

	principals := previewPrincipals("oc_1", "ou_1")
	if len(principals) != 2 || principals[0].Type != "user" || principals[1].Type != "chat" {
		t.Fatalf("previewPrincipals() = %+v", principals)
	}
	if roots := previewAllowedRoots(".", ".", ".."); len(roots) == 0 {
		t.Fatalf("previewAllowedRoots() = %+v, want resolved roots", roots)
	}
	if got := previewPathCandidates("/tmp/a.md", nil); len(got) != 1 || got[0] != "/tmp/a.md" {
		t.Fatalf("previewPathCandidates(abs) = %+v", got)
	}
	if !previewPathWithinRoot("/tmp/root/a.md", "/tmp/root") || previewPathWithinRoot("/tmp/other/a.md", "/tmp/root") {
		t.Fatal("previewPathWithinRoot() returned unexpected result")
	}
	if got := sanitizePreviewFileComponent(" bad name!.md "); got != "bad-name--md" {
		t.Fatalf("sanitizePreviewFileComponent() = %q", got)
	}

	name := previewFileName("/tmp/docs/guide.md", strings.Repeat("a", 64), time.Unix(1700000000, 0))
	if !strings.HasPrefix(name, previewManagedFilePrefix) {
		t.Fatalf("previewFileName() = %q, want managed prefix", name)
	}
	if ts, ok := previewManagedFileTime(name); !ok || ts.IsZero() {
		t.Fatalf("previewManagedFileTime() = %v, %v", ts, ok)
	}
	if got := stringPtrValue(nil); got != "" {
		t.Fatalf("stringPtrValue(nil) = %q, want empty", got)
	}
	if got := NewLarkDrivePreviewAPI(nil); got != nil {
		t.Fatalf("NewLarkDrivePreviewAPI(nil) = %+v, want nil", got)
	}
}

func TestDriveMarkdownPreviewerResolveAndPermissionHelpers(t *testing.T) {
	root := t.TempDir()
	outsideRoot := t.TempDir()
	valid := filepath.Join(root, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(valid), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(valid, []byte("# guide\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(valid) error = %v", err)
	}
	empty := filepath.Join(root, "empty.md")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("WriteFile(empty) error = %v", err)
	}
	outside := filepath.Join(outsideRoot, "secret.md")
	if err := os.WriteFile(outside, []byte("# secret\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(outside) error = %v", err)
	}
	escaped := filepath.Join(root, "docs", "escaped.md")
	if err := os.Symlink(outside, escaped); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	api := &fakePreviewAPI{}
	p := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{ProcessCWD: root, MaxFileBytes: 4})
	if resolved, ok, err := p.resolveMarkdownPath("./docs/guide.md#L12", MarkdownPreviewRequest{WorkspaceCWD: root}); err != nil || !ok || resolved != valid {
		t.Fatalf("resolveMarkdownPath() = %q, %v, %v", resolved, ok, err)
	}
	if _, ok, err := p.resolveMarkdownPath("../outside.txt", MarkdownPreviewRequest{WorkspaceCWD: root}); err != nil || ok {
		t.Fatalf("resolveMarkdownPath(outside) = %v, %v, want false", ok, err)
	}
	if _, ok, err := p.resolveMarkdownPath("./docs/escaped.md", MarkdownPreviewRequest{WorkspaceCWD: root}); err != nil || ok {
		t.Fatalf("resolveMarkdownPath(symlink escape) = %v, %v, want false", ok, err)
	}

	p = NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{ProcessCWD: root, MaxFileBytes: 1024})
	if _, _, err := p.materializeMarkdownTargetLocked(context.Background(), "./empty.md", MarkdownPreviewRequest{WorkspaceCWD: root, ChatID: "oc_1", UserID: "ou_1"}, previewPrincipals("oc_1", "ou_1")); err == nil || !strings.Contains(err.Error(), "skip empty") {
		t.Fatalf("materializeMarkdownTargetLocked(empty) error = %v", err)
	}

	p = NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{ProcessCWD: root, MaxFileBytes: 1})
	if _, _, err := p.materializeMarkdownTargetLocked(context.Background(), "./docs/guide.md", MarkdownPreviewRequest{WorkspaceCWD: root, ChatID: "oc_1", UserID: "ou_1"}, previewPrincipals("oc_1", "ou_1")); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("materializeMarkdownTargetLocked(large) error = %v", err)
	}

	shared := map[string]bool{}
	if err := ensurePreviewPermissions(context.Background(), api, "token-1", previewFileType, shared, previewPrincipals("oc_1", "ou_1")); err != nil {
		t.Fatalf("ensurePreviewPermissions() error = %v", err)
	}
	if len(api.grantCalls) != 2 {
		t.Fatalf("grantCalls = %+v, want user + chat", api.grantCalls)
	}
	if err := ensurePreviewPermissions(context.Background(), api, "", previewFileType, shared, nil); err == nil {
		t.Fatal("expected missing token to fail")
	}
}
