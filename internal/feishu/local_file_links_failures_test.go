package feishu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type driveFailureRT struct {
	path string
	body string
}

func (r driveFailureRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
		return (&adapterMockAPI{}).RoundTrip(req)
	}
	if req.URL.Path == r.path {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(r.body)),
			Request:    req,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"code":404}`)),
		Request:    req,
	}, nil
}

type failingPreviewAPI struct{}

func (failingPreviewAPI) CreateFolder(context.Context, string, string) (previewRemoteNode, error) {
	return previewRemoteNode{}, errors.New("create failed")
}
func (failingPreviewAPI) ListFiles(context.Context, string) ([]previewRemoteNode, error) {
	return nil, errors.New("list failed")
}
func (failingPreviewAPI) UploadFile(context.Context, string, string, string) (string, error) {
	return "", errors.New("upload failed")
}
func (failingPreviewAPI) QueryMetaURL(context.Context, string, string) (string, error) {
	return "", errors.New("meta failed")
}
func (failingPreviewAPI) GrantPermission(context.Context, string, string, previewPrincipal) error {
	return errors.New("grant failed")
}
func (failingPreviewAPI) DeleteFile(context.Context, string, string) error {
	return errors.New("delete failed")
}

func TestDriveLocalFileLinkRewriterFailurePaths(t *testing.T) {
	p := NewDriveLocalFileLinkRewriter(failingPreviewAPI{}, LocalFileLinkConfig{})
	if _, err := p.CleanupBefore(context.Background(), time.Now()); err == nil || !strings.Contains(err.Error(), "list failed") {
		t.Fatalf("CleanupBefore(list failure) error = %v", err)
	}
	if _, ok, err := p.materializePreviewTargetLocked(context.Background(), "missing.md", LocalFileLinkRewriteRequest{WorkspaceCWD: t.TempDir()}, nil); err != nil || ok {
		t.Fatalf("materializePreviewTargetLocked(missing) = %v, %v", ok, err)
	}
}

func TestLarkDrivePreviewAPIFailureResponses(t *testing.T) {
	client := lark.NewClient("app", "secret", lark.WithOpenBaseUrl("https://mock.feishu.test"), lark.WithHttpClient(&http.Client{Transport: driveFailureRT{
		path: "/open-apis/drive/v1/files/create_folder",
		body: `{"code":1,"msg":"bad"}`,
	}}))
	api := NewLarkDrivePreviewAPI(client)
	if _, err := api.CreateFolder(context.Background(), "name", ""); err == nil {
		t.Fatal("expected CreateFolder failure")
	}
}
