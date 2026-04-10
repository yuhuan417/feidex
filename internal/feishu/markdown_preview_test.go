package feishu

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
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
	if api.createFolderCalls != 2 || api.uploadCalls != 1 || api.queryCalls != 1 {
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
	children := api.children["folder-1"]
	if len(children) != 1 || children[0].Type != previewFolderType {
		t.Fatalf("expected one artifact folder under root, got %#v", children)
	}
	children[0].CreatedTime = time.Now().Add(-8 * 24 * time.Hour)
	api.children["folder-1"] = children

	result, err := previewer.CleanupBefore(context.Background(), time.Now().Add(-7*24*time.Hour))
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
