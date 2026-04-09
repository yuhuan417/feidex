package feishu

import (
	"context"
	"strconv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakePreviewAPI struct {
	createFolderCalls int
	listCalls         []string
	uploadCalls       int
	queryCalls        int
	grantCalls        []previewPrincipal
	deleteCalls       []string
	root              *previewRemoteNode
	files             []previewRemoteNode
}

func (f *fakePreviewAPI) CreateFolder(_ context.Context, name, _ string) (previewRemoteNode, error) {
	f.createFolderCalls++
	node := previewRemoteNode{
		Token: "folder-1",
		URL:   "https://drive.example/folder-1",
		Type:  previewFolderType,
		Name:  name,
	}
	f.root = &node
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
	case "folder-1":
		return append([]previewRemoteNode(nil), f.files...), nil
	default:
		return nil, nil
	}
}

func (f *fakePreviewAPI) UploadFile(_ context.Context, _ string, fileName string, _ []byte) (string, error) {
	f.uploadCalls++
	token := "file-" + strconv.Itoa(f.uploadCalls)
	f.files = append(f.files, previewRemoteNode{
		Token: token,
		URL:   "https://drive.example/" + token,
		Type:  previewFileType,
		Name:  fileName,
	})
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
	next := make([]previewRemoteNode, 0, len(f.files))
	for _, node := range f.files {
		if node.Token == token {
			continue
		}
		next = append(next, node)
	}
	f.files = next
	return nil
}

func TestDriveMarkdownPreviewerRewriteTextReplacesLocalMarkdownLinks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	api := &fakePreviewAPI{}
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		ProcessCWD: root,
	})

	got, err := previewer.RewriteText(context.Background(), MarkdownPreviewRequest{
		Text:         "请看 [README](README.md) 和 [README2](README.md)",
		WorkspaceCWD: root,
		ChatID:       "oc_123",
		UserID:       "ou_123",
	})
	if err != nil {
		t.Fatalf("RewriteText returned error: %v", err)
	}
	if !strings.Contains(got, "https://drive.example/file-1") {
		t.Fatalf("expected rewritten preview url, got %q", got)
	}
	if api.createFolderCalls != 1 || api.uploadCalls != 1 || api.queryCalls != 1 {
		t.Fatalf("unexpected drive api usage: %#v", api)
	}
	if len(api.grantCalls) != 2 {
		t.Fatalf("expected user + chat permissions, got %#v", api.grantCalls)
	}

	gotAgain, err := previewer.RewriteText(context.Background(), MarkdownPreviewRequest{
		Text:         "请看 [README](README.md)",
		WorkspaceCWD: root,
		ChatID:       "oc_123",
		UserID:       "ou_123",
	})
	if err != nil {
		t.Fatalf("RewriteText second call returned error: %v", err)
	}
	if !strings.Contains(gotAgain, "https://drive.example/file-2") {
		t.Fatalf("expected fresh upload on second rewrite, got %q", gotAgain)
	}
	if api.uploadCalls != 2 {
		t.Fatalf("expected one upload per rewrite call, upload calls=%d", api.uploadCalls)
	}
}

func TestDriveMarkdownPreviewerCleanupBeforeDeletesExpiredFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	api := &fakePreviewAPI{}
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		ProcessCWD: root,
	})

	if _, err := previewer.RewriteText(context.Background(), MarkdownPreviewRequest{
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
	api.files[0].Name = previewFileName(filepath.Join(root, "README.md"), strings.Repeat("a", 64), time.Now().Add(-8*24*time.Hour))

	result, err := previewer.CleanupBefore(context.Background(), time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("CleanupBefore returned error: %v", err)
	}
	if result.DeletedFileCount != 1 {
		t.Fatalf("expected one deleted preview file, got %#v", result)
	}
	if len(api.deleteCalls) != 1 || api.deleteCalls[0] != "file-1" {
		t.Fatalf("unexpected delete calls: %#v", api.deleteCalls)
	}
	if len(api.files) != 0 {
		t.Fatalf("expected remote preview listing to be empty after cleanup, got %#v", api.files)
	}
}
