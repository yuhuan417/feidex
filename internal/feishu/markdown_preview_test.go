package feishu

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakePreviewAPI struct {
	createFolderCalls int
	uploadCalls       int
	queryCalls        int
	grantCalls        []previewPrincipal
	deleteCalls       []string
}

func (f *fakePreviewAPI) CreateFolder(_ context.Context, _ string, _ string) (previewRemoteNode, error) {
	f.createFolderCalls++
	return previewRemoteNode{Token: "folder-1", URL: "https://drive.example/folder-1", Type: previewFolderType}, nil
}

func (f *fakePreviewAPI) UploadFile(_ context.Context, _ string, _ string, _ []byte) (string, error) {
	f.uploadCalls++
	return "file-1", nil
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
	return nil
}

func TestDriveMarkdownPreviewerRewriteTextReplacesLocalMarkdownLinks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	api := &fakePreviewAPI{}
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		StatePath:  filepath.Join(root, "preview-state.json"),
		ProcessCWD: root,
	})

	got, err := previewer.RewriteText(context.Background(), MarkdownPreviewRequest{
		Text:         "请看 [README](README.md)",
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
	if gotAgain != got {
		t.Fatalf("expected stable rewritten output, got %q", gotAgain)
	}
	if api.uploadCalls != 1 {
		t.Fatalf("expected cached preview file reuse, upload calls=%d", api.uploadCalls)
	}
}

func TestDriveMarkdownPreviewerCleanupBeforeDeletesExpiredFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	api := &fakePreviewAPI{}
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		StatePath:  filepath.Join(root, "preview-state.json"),
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

	previewer.mu.Lock()
	for _, record := range previewer.state.Files {
		record.LastUsedAt = time.Now().Add(-8 * 24 * time.Hour)
	}
	if err := previewer.saveStateLocked(); err != nil {
		previewer.mu.Unlock()
		t.Fatalf("saveStateLocked: %v", err)
	}
	previewer.mu.Unlock()

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

	previewer.mu.Lock()
	defer previewer.mu.Unlock()
	if len(previewer.state.Files) != 0 {
		t.Fatalf("expected preview state to be empty after cleanup, got %#v", previewer.state.Files)
	}
}
