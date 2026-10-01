package feishu

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type fakePreviewAPI struct {
	createFolderCalls int
	listCalls         []string
	uploadCalls       int
	queryCalls        int
	grantCalls        []previewPrincipal
	deleteCalls       []string
	root              *previewRemoteNode
	children          map[string][]previewRemoteNode
	files             []previewRemoteNode
}

func (f *fakePreviewAPI) CreateFolder(_ context.Context, name, parentToken string) (previewRemoteNode, error) {
	f.createFolderCalls++
	node := previewRemoteNode{
		Token:       "folder-" + strconv.Itoa(f.createFolderCalls),
		URL:         "https://drive.example/folder-" + strconv.Itoa(f.createFolderCalls),
		Type:        previewFolderType,
		Name:        name,
		CreatedTime: time.Now(),
	}
	if f.children == nil {
		f.children = map[string][]previewRemoteNode{}
	}
	if strings.TrimSpace(parentToken) == "" {
		f.root = &node
	} else {
		f.children[parentToken] = append(f.children[parentToken], node)
	}
	return node, nil
}

func (f *fakePreviewAPI) ListFiles(_ context.Context, folderToken string) ([]previewRemoteNode, error) {
	f.listCalls = append(f.listCalls, folderToken)
	switch strings.TrimSpace(folderToken) {
	case "":
		if f.root == nil {
			return nil, nil
		}
		return []previewRemoteNode{*f.root}, nil
	default:
		return append([]previewRemoteNode(nil), f.children[strings.TrimSpace(folderToken)]...), nil
	}
}

func (f *fakePreviewAPI) UploadFile(_ context.Context, parentToken, fileName, _ string) (string, error) {
	f.uploadCalls++
	token := "file-" + strconv.Itoa(f.uploadCalls)
	node := previewRemoteNode{
		Token:       token,
		URL:         "https://drive.example/" + token,
		Type:        previewFileType,
		Name:        fileName,
		CreatedTime: time.Now(),
	}
	f.files = append(f.files, node)
	if f.children == nil {
		f.children = map[string][]previewRemoteNode{}
	}
	f.children[strings.TrimSpace(parentToken)] = append(f.children[strings.TrimSpace(parentToken)], node)
	return token, nil
}

func (f *fakePreviewAPI) QueryMetaURL(_ context.Context, token, _ string) (string, error) {
	f.queryCalls++
	return "https://drive.example/" + token, nil
}

func (f *fakePreviewAPI) GrantPermission(_ context.Context, _ string, _ string, principal previewPrincipal) error {
	f.grantCalls = append(f.grantCalls, principal)
	return nil
}

func (f *fakePreviewAPI) DeleteFile(_ context.Context, token, _ string) error {
	f.deleteCalls = append(f.deleteCalls, token)
	if children, ok := f.children[token]; ok {
		childTokens := map[string]struct{}{}
		for _, node := range children {
			childTokens[node.Token] = struct{}{}
		}
		delete(f.children, token)
		nextFiles := make([]previewRemoteNode, 0, len(f.files))
		for _, node := range f.files {
			if _, exists := childTokens[node.Token]; exists {
				continue
			}
			nextFiles = append(nextFiles, node)
		}
		f.files = nextFiles
	}
	next := make([]previewRemoteNode, 0, len(f.files))
	for _, node := range f.files {
		if node.Token == token {
			continue
		}
		next = append(next, node)
	}
	f.files = next
	for parent, nodes := range f.children {
		filtered := nodes[:0]
		for _, node := range nodes {
			if node.Token == token {
				continue
			}
			filtered = append(filtered, node)
		}
		f.children[parent] = append([]previewRemoteNode(nil), filtered...)
	}
	if f.root != nil && f.root.Token == token {
		f.root = nil
	}
	return nil
}

func TestDriveLocalFileLinkRewriterRewriteTextReplacesLocalMarkdownLinks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	api := &fakePreviewAPI{}
	rewriter := NewDriveLocalFileLinkRewriter(api, LocalFileLinkConfig{
		ProcessCWD: root,
	})

	got, err := rewriter.RewriteText(context.Background(), LocalFileLinkRewriteRequest{
		Text:         "请看 [README](README.md) 和 [README2](README.md)",
		WorkspaceCWD: root,
		ChatID:       "oc_123",
		UserID:       "ou_123",
	})
	if err != nil {
		t.Fatalf("RewriteText returned error: %v", err)
	}
	if !strings.Contains(got, "[README.md](https://drive.example/file-1)") {
		t.Fatalf("expected rewritten preview link with full path label, got %q", got)
	}
	if api.createFolderCalls != 2 || api.uploadCalls != 1 || api.queryCalls != 1 {
		t.Fatalf("unexpected drive api usage: %#v", api)
	}
	if len(api.grantCalls) != 2 {
		t.Fatalf("expected user + chat permissions, got %#v", api.grantCalls)
	}

	gotAgain, err := rewriter.RewriteText(context.Background(), LocalFileLinkRewriteRequest{
		Text:         "请看 [README](README.md)",
		WorkspaceCWD: root,
		ChatID:       "oc_123",
		UserID:       "ou_123",
	})
	if err != nil {
		t.Fatalf("RewriteText second call returned error: %v", err)
	}
	if !strings.Contains(gotAgain, "[README.md](https://drive.example/file-2)") {
		t.Fatalf("expected fresh upload with full path label on second rewrite, got %q", gotAgain)
	}
	if api.uploadCalls != 2 {
		t.Fatalf("expected one upload per rewrite call, upload calls=%d", api.uploadCalls)
	}
}

func TestDriveLocalFileLinkRewriterRewriteTextReplacesLocalNonMarkdownLinks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write go file: %v", err)
	}
	api := &fakePreviewAPI{}
	rewriter := NewDriveLocalFileLinkRewriter(api, LocalFileLinkConfig{
		ProcessCWD: root,
	})

	got, err := rewriter.RewriteText(context.Background(), LocalFileLinkRewriteRequest{
		Text:         "请看 [Main](main.go:12) 和 [MainAgain](main.go:12)",
		WorkspaceCWD: root,
		ChatID:       "oc_123",
		UserID:       "ou_123",
	})
	if err != nil {
		t.Fatalf("RewriteText returned error: %v", err)
	}
	if !strings.Contains(got, "[main.go:12](https://drive.example/file-1)") {
		t.Fatalf("expected rewritten local file path link, got %q", got)
	}
	if api.createFolderCalls != 2 || api.uploadCalls != 1 || api.queryCalls != 1 {
		t.Fatalf("unexpected drive api usage for non-markdown link: %#v", api)
	}
}

func TestDriveLocalFileLinkRewriterCleanupBeforeDeletesExpiredFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	api := &fakePreviewAPI{}
	rewriter := NewDriveLocalFileLinkRewriter(api, LocalFileLinkConfig{
		ProcessCWD: root,
	})

	if _, err := rewriter.RewriteText(context.Background(), LocalFileLinkRewriteRequest{
		Text:         "请看 [README](README.md)",
		WorkspaceCWD: root,
		ChatID:       "oc_123",
		UserID:       "ou_123",
	}); err != nil {
		t.Fatalf("RewriteText returned error: %v", err)
	}

	if len(api.files) != 1 {
		t.Fatalf("expected one uploaded preview file, got %#v", api.files)
	}
	children := api.children["folder-1"]
	if len(children) != 1 || children[0].Type != previewFolderType {
		t.Fatalf("expected one artifact folder under root, got %#v", children)
	}
	children[0].CreatedTime = time.Now().Add(-8 * 24 * time.Hour)
	api.children["folder-1"] = children

	result, err := rewriter.CleanupBefore(context.Background(), time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("CleanupBefore returned error: %v", err)
	}
	if result.DeletedFileCount != 1 {
		t.Fatalf("expected one deleted preview file, got %#v", result)
	}
	if len(api.deleteCalls) != 1 || api.deleteCalls[0] != "folder-2" {
		t.Fatalf("unexpected delete calls: %#v", api.deleteCalls)
	}
	if len(api.files) != 0 {
		t.Fatalf("expected remote preview listing to be empty after cleanup, got %#v", api.files)
	}
}

// --- merged from local_file_links_more_more_test.go ---

func TestLocalFileLinkAdditionalHelpers(t *testing.T) {
	if got := (*driveAPIError)(nil).Error(); got != "" {
		t.Fatalf("nil driveAPIError Error() = %q", got)
	}
	if _, ok := previewUserPrincipal("on_123"); !ok {
		t.Fatal("previewUserPrincipal(unionid) should succeed")
	}
	if _, ok := previewChatPrincipal(""); ok {
		t.Fatal("previewChatPrincipal(empty) should fail")
	}
	if _, ok := previewManagedFileTime("bad"); ok {
		t.Fatal("previewManagedFileTime(bad) should fail")
	}

	api := &fakePreviewAPI{}
	p := NewDriveLocalFileLinkRewriter(api, LocalFileLinkConfig{})
	p.store.root = &previewFolderRecord{Token: "folder-1", URL: "https://drive.example/folder-1"}
	root, err := p.ensureRootFolderLocked(context.Background())
	if err != nil || root.Token != "folder-1" {
		t.Fatalf("ensureRootFolderLocked(existing) = %+v, %v", root, err)
	}
	if got, err := p.listRootFoldersLocked(context.Background()); err != nil || got == nil {
		t.Fatalf("listRootFoldersLocked() = %+v, %v", got, err)
	}
	api.root = &previewRemoteNode{Token: "folder-2", URL: "https://drive.example/folder-2", Type: previewFolderType, Name: defaultPreviewRootFolderName}
	p = NewDriveLocalFileLinkRewriter(api, LocalFileLinkConfig{})
	root, err = p.ensureRootFolderLocked(context.Background())
	if err != nil || root.Token != "folder-2" {
		t.Fatalf("ensureRootFolderLocked(list existing) = %+v, %v", root, err)
	}
	shared := map[string]bool{"openid:ou_1": true}
	if err := ensurePreviewPermissions(context.Background(), api, "token-1", previewFileType, shared, []previewPrincipal{{Key: "openid:ou_1", MemberType: "openid", MemberID: "ou_1", Type: "user"}}); err != nil {
		t.Fatalf("ensurePreviewPermissions(shared) error = %v", err)
	}
	if previewPathWithinAnyRoot("/tmp/outside", []string{"/tmp/root"}) {
		t.Fatal("previewPathWithinAnyRoot() should reject outside path")
	}
	if previewPathWithinRoot("/tmp/root", ".") {
		t.Fatal("previewPathWithinRoot(dot root) should reject")
	}
	if got := sanitizePreviewFileComponent("!!!"); got != "preview" {
		t.Fatalf("sanitizePreviewFileComponent(empty) = %q, want preview", got)
	}
}

// --- merged from local_file_links_more_test.go ---

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
	if got := formatPreviewLinkReplacement("./docs/guide.md:12", "https://drive.example/file-1", ""); got != "[docs/guide.md:12](https://drive.example/file-1)" {
		t.Fatalf("formatPreviewLinkReplacement() = %q", got)
	}
	if got := formatPreviewLinkReplacement("/repo/docs/guide.md:12", "https://drive.example/file-1", "/repo"); got != "[docs/guide.md:12](https://drive.example/file-1)" {
		t.Fatalf("formatPreviewLinkReplacement(abs workspace path) = %q", got)
	}
	if got := formatPreviewLinkReplacement("./cmd/main.go:9", "https://drive.example/file-2", ""); got != "[cmd/main.go:9](https://drive.example/file-2)" {
		t.Fatalf("formatPreviewLinkReplacement(non-markdown) = %q", got)
	}
	if got := formatPreviewLinkReplacement("/repo/docs/guide.md#L12", "https://drive.example/file-3", "/repo"); got != "[docs/guide.md:12](https://drive.example/file-3)" {
		t.Fatalf("formatPreviewLinkReplacement(line anchor) = %q", got)
	}
	if got := formatPreviewLinkReplacement("/repo/docs/guide.md#L12C3", "https://drive.example/file-4", "/repo"); got != "[docs/guide.md:12:3](https://drive.example/file-4)" {
		t.Fatalf("formatPreviewLinkReplacement(line+column anchor) = %q", got)
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
