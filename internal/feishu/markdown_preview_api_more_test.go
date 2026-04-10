package feishu

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type drivePathRT struct {
	responses map[string]string
}

func (r drivePathRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
		return (&adapterMockAPI{}).RoundTrip(req)
	}
	body := r.responses[req.URL.Path]
	if body == "" {
		body = `{"code":404}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func TestLarkDrivePreviewAPIMissingDataBranches(t *testing.T) {
	makeAPI := func(path, body string) previewDriveAPI {
		client := lark.NewClient("app", "secret", lark.WithOpenBaseUrl("https://mock.feishu.test"), lark.WithHttpClient(&http.Client{Transport: drivePathRT{responses: map[string]string{path: body}}}))
		return NewLarkDrivePreviewAPI(client)
	}

	if _, err := makeAPI("/open-apis/drive/v1/files/create_folder", `{"code":0}`).CreateFolder(context.Background(), "name", ""); err == nil {
		t.Fatal("expected CreateFolder missing data to fail")
	}
	localPath := filepath.Join(t.TempDir(), "name.md")
	if err := os.WriteFile(localPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(name.md) error = %v", err)
	}
	if _, err := makeAPI("/open-apis/drive/v1/files/upload_prepare", `{"code":0}`).UploadFile(context.Background(), "folder", "name.md", localPath); err == nil {
		t.Fatal("expected UploadFile missing data to fail")
	}
	if _, err := makeAPI("/open-apis/drive/v1/metas/batch_query", `{"code":0,"data":{"metas":[]}}`).QueryMetaURL(context.Background(), "token", previewFileType); err == nil {
		t.Fatal("expected QueryMetaURL missing metas to fail")
	}
	if err := makeAPI("/open-apis/drive/v1/permissions/token/members", `{"code":1,"msg":"bad"}`).GrantPermission(context.Background(), "token", previewFileType, previewPrincipal{MemberType: "openid", MemberID: "ou_1", Type: "user"}); err == nil {
		t.Fatal("expected GrantPermission non-success to fail")
	}
	if err := makeAPI("/open-apis/drive/v1/files/token", `{"code":1,"msg":"bad"}`).DeleteFile(context.Background(), "token", previewFileType); err == nil {
		t.Fatal("expected DeleteFile non-success to fail")
	}
}
