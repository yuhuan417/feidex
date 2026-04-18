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

func TestLocalFileLinkHelpers(t *testing.T) {
	if got := (&driveAPIError{Code: 403, Msg: " denied "}).Error(); got != "feishu drive api error 403: denied" {
		t.Fatalf("driveAPIError.Error() = %q", got)
	}

	a := &Adapter{}
	text, err := a.RewriteLocalFileLinks(context.Background(), LocalFileLinkRewriteRequest{Text: "hello"})
	if err != nil || text != "hello" {
		t.Fatalf("RewriteLocalFileLinks(nil rewriter) = %q, %v", text, err)
	}
	if _, err := a.CleanupLocalFileLinksBefore(context.Background(), time.Now()); err != nil {
		t.Fatalf("CleanupLocalFileLinksBefore(nil rewriter) error = %v", err)
	}

	a = &Adapter{client: lark.NewClient("app", "secret"), localFileLinkProcessCWD: "/repo"}
	rewriter := a.ensureLocalFileLinkRewriter()
	if rewriter == nil || rewriter.config.ProcessCWD != "/repo" {
		t.Fatalf("ensureLocalFileLinkRewriter() = %+v, want configured rewriter", rewriter)
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
	if got := formatPreviewLinkReplacement("./docs/guide.md:12", "https://drive.example/file-1", ""); got != "`docs/guide.md:12` [guide.md](https://drive.example/file-1)" {
		t.Fatalf("formatPreviewLinkReplacement() = %q", got)
	}
	if got := formatPreviewLinkReplacement("/repo/docs/guide.md:12", "https://drive.example/file-1", "/repo"); got != "`docs/guide.md:12` [guide.md](https://drive.example/file-1)" {
		t.Fatalf("formatPreviewLinkReplacement(abs workspace path) = %q", got)
	}
	if got := formatPreviewLinkReplacement("./cmd/main.go:9", "https://drive.example/file-2", ""); got != "`cmd/main.go:9` [main.go](https://drive.example/file-2)" {
		t.Fatalf("formatPreviewLinkReplacement(non-markdown) = %q", got)
	}
}

func TestDriveLocalFileLinkRewriterResolveAndPermissionHelpers(t *testing.T) {
	root := t.TempDir()
	outsideRoot := t.TempDir()
	valid := filepath.Join(root, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(valid), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(valid, []byte("# guide\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(valid) error = %v", err)
	}
	validGo := filepath.Join(root, "cmd", "main.go")
	if err := os.MkdirAll(filepath.Dir(validGo), 0o755); err != nil {
		t.Fatalf("MkdirAll(validGo) error = %v", err)
	}
	if err := os.WriteFile(validGo, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(validGo) error = %v", err)
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
	p := NewDriveLocalFileLinkRewriter(api, LocalFileLinkConfig{ProcessCWD: root, MaxFileBytes: 4})
	if resolved, ok, err := p.resolvePreviewPath("./docs/guide.md#L12", LocalFileLinkRewriteRequest{WorkspaceCWD: root}); err != nil || !ok || resolved != valid {
		t.Fatalf("resolvePreviewPath(markdown) = %q, %v, %v", resolved, ok, err)
	}
	if resolved, ok, err := p.resolvePreviewPath("./cmd/main.go:9", LocalFileLinkRewriteRequest{WorkspaceCWD: root}); err != nil || !ok || resolved != validGo {
		t.Fatalf("resolvePreviewPath(non-markdown) = %q, %v, %v", resolved, ok, err)
	}
	if _, ok, err := p.resolvePreviewPath("../outside.txt", LocalFileLinkRewriteRequest{WorkspaceCWD: root}); err != nil || ok {
		t.Fatalf("resolvePreviewPath(outside) = %v, %v, want false", ok, err)
	}
	if _, ok, err := p.resolvePreviewPath("./docs/escaped.md", LocalFileLinkRewriteRequest{WorkspaceCWD: root}); err != nil || ok {
		t.Fatalf("resolvePreviewPath(symlink escape) = %v, %v, want false", ok, err)
	}

	p = NewDriveLocalFileLinkRewriter(api, LocalFileLinkConfig{ProcessCWD: root, MaxFileBytes: 1024})
	if _, _, err := p.materializePreviewTargetLocked(context.Background(), "./empty.md", LocalFileLinkRewriteRequest{WorkspaceCWD: root, ChatID: "oc_1", UserID: "ou_1"}, previewPrincipals("oc_1", "ou_1")); err == nil || !strings.Contains(err.Error(), "skip empty") {
		t.Fatalf("materializePreviewTargetLocked(empty) error = %v", err)
	}

	p = NewDriveLocalFileLinkRewriter(api, LocalFileLinkConfig{ProcessCWD: root, MaxFileBytes: 1})
	if _, _, err := p.materializePreviewTargetLocked(context.Background(), "./docs/guide.md", LocalFileLinkRewriteRequest{WorkspaceCWD: root, ChatID: "oc_1", UserID: "ou_1"}, previewPrincipals("oc_1", "ou_1")); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("materializePreviewTargetLocked(large) error = %v", err)
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
