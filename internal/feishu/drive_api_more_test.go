package feishu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type driveAPIMock struct {
	mu       sync.Mutex
	requests []string
}

func (m *driveAPIMock) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, req.Method+" "+req.URL.Path)
	m.mu.Unlock()

	writeJSON := func(v any) (*http.Response, error) {
		body, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Request:    req,
		}, nil
	}

	switch req.URL.Path {
	case "/open-apis/auth/v3/tenant_access_token/internal":
		return writeJSON(map[string]any{"code": 0, "tenant_access_token": "tenant-token"})
	case "/open-apis/drive/v1/files/create_folder":
		return writeJSON(map[string]any{"code": 0, "data": map[string]any{"token": "folder-1", "url": "https://drive.example/folder-1"}})
	case "/open-apis/drive/v1/files":
		return writeJSON(map[string]any{
			"code": 0,
			"data": map[string]any{
				"files": []map[string]any{
					{"token": "folder-1", "url": "https://drive.example/folder-1", "type": "folder", "name": defaultArtifactRootFolderName, "created_time": "1700000000"},
					{"token": "file-1", "url": "https://drive.example/file-1", "type": "file", "name": "preview.md", "created_time": "1700000001"},
				},
				"has_more": false,
			},
		})
	case "/open-apis/drive/v1/files/upload_prepare":
		return writeJSON(map[string]any{"code": 0, "data": map[string]any{"upload_id": "upload-1", "block_size": 4194304, "block_num": 1}})
	case "/open-apis/drive/v1/files/upload_part":
		return writeJSON(map[string]any{"code": 0})
	case "/open-apis/drive/v1/files/upload_finish":
		return writeJSON(map[string]any{"code": 0, "data": map[string]any{"file_token": "file-1"}})
	case "/open-apis/drive/v1/metas/batch_query":
		return writeJSON(map[string]any{"code": 0, "data": map[string]any{"metas": []map[string]any{{"url": "https://drive.example/file-1"}}}})
	case "/open-apis/drive/v1/permissions/token-1/members":
		return writeJSON(map[string]any{"code": 0, "data": map[string]any{"member": map[string]any{"member_id": "ou_1"}}})
	case "/open-apis/drive/v1/files/token-1":
		return writeJSON(map[string]any{"code": 0})
	default:
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":404}`)),
			Request:    req,
		}, nil
	}
}

func TestLarkDrivePreviewAPIMethods(t *testing.T) {
	mock := &driveAPIMock{}
	client := lark.NewClient(
		"app-id",
		"app-secret",
		lark.WithOpenBaseUrl("https://mock.feishu.test"),
		lark.WithHttpClient(&http.Client{Transport: mock}),
	)
	api := NewLarkDrivePreviewAPI(client)
	if api == nil {
		t.Fatal("expected preview api")
	}

	node, err := api.CreateFolder(context.Background(), defaultPreviewRootFolderName, "")
	if err != nil || node.Token != "folder-1" {
		t.Fatalf("CreateFolder() = %+v, %v", node, err)
	}
	nodes, err := api.ListFiles(context.Background(), "")
	if err != nil || len(nodes) != 2 {
		t.Fatalf("ListFiles() = %+v, %v", nodes, err)
	}
	localPath := filepath.Join(t.TempDir(), "preview.md")
	if err := os.WriteFile(localPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(preview.md) error = %v", err)
	}
	token, err := api.UploadFile(context.Background(), "folder-1", "preview.md", localPath)
	if err != nil || token != "file-1" {
		t.Fatalf("UploadFile() = %q, %v", token, err)
	}
	url, err := api.QueryMetaURL(context.Background(), "file-1", previewFileType)
	if err != nil || url != "https://drive.example/file-1" {
		t.Fatalf("QueryMetaURL() = %q, %v", url, err)
	}
	if err := api.GrantPermission(context.Background(), "token-1", previewFileType, previewPrincipal{MemberType: "openid", MemberID: "ou_1", Type: "user"}); err != nil {
		t.Fatalf("GrantPermission() error = %v", err)
	}
	if err := api.DeleteFile(context.Background(), "token-1", previewFileType); err != nil {
		t.Fatalf("DeleteFile() error = %v", err)
	}
}
