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

func TestAdapterHelperNegativeBranches(t *testing.T) {
	if _, ok := extractImageAttachment(nil); ok {
		t.Fatal("extractImageAttachment(nil) should fail")
	}
	if _, ok := extractImageAttachment(strPtr(`bad`)); ok {
		t.Fatal("extractImageAttachment(invalid) should fail")
	}
	if _, ok := extractFileAttachment(nil); ok {
		t.Fatal("extractFileAttachment(nil) should fail")
	}
	if _, ok := extractAudioAttachment(nil); ok {
		t.Fatal("extractAudioAttachment(nil) should fail")
	}
	if got := detectUploadFileType("sheet.xlsx"); got != "xls" {
		t.Fatalf("detectUploadFileType(xlsx) = %q, want xls", got)
	}
	if got := detectUploadFileType("slides.pptx"); got != "ppt" {
		t.Fatalf("detectUploadFileType(pptx) = %q, want ppt", got)
	}
	if got := sanitizeDownloadedFileName("/"); got != "" {
		t.Fatalf("sanitizeDownloadedFileName(root) = %q, want empty", got)
	}
}

type uploadNilKeyAPI struct{}

func (uploadNilKeyAPI) RoundTrip(req *http.Request) (*http.Response, error) {
	body := `{"code":0,"tenant_access_token":"tenant-token","expire":7200}`
	switch req.URL.Path {
	case "/open-apis/im/v1/images":
		body = `{"code":0,"data":{}}`
	case "/open-apis/im/v1/files":
		body = `{"code":0,"data":{}}`
	case "/open-apis/auth/v3/tenant_access_token/internal":
	default:
		body = `{"code":0,"msg":"ok","data":{"message_id":"reply-1"}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func TestAdapterUploadNilKeyErrors(t *testing.T) {
	client := lark.NewClient("app-id", "app-secret", lark.WithOpenBaseUrl("https://mock.feishu.test"), lark.WithHttpClient(&http.Client{Transport: uploadNilKeyAPI{}}))
	a := &Adapter{client: client, reactions: map[string]string{}}

	image := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(image, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile(image) error = %v", err)
	}
	if err := a.replyLocalImage(context.Background(), "msg-1", image, false); err == nil {
		t.Fatal("expected replyLocalImage nil key to fail")
	}

	filePath := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(file) error = %v", err)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Stat(file) error = %v", err)
	}
	if err := a.replyLocalUploadedFile(context.Background(), "msg-1", filePath, info, "file", false); err == nil {
		t.Fatal("expected replyLocalUploadedFile nil key to fail")
	}
}
