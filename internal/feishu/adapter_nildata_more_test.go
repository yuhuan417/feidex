package feishu

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type adapterNilDataAPI struct{}

func (adapterNilDataAPI) RoundTrip(req *http.Request) (*http.Response, error) {
	body := `{"code":0,"msg":"ok"}`
	switch req.URL.Path {
	case "/open-apis/auth/v3/tenant_access_token/internal":
		body = `{"code":0,"tenant_access_token":"tenant-token","expire":7200}`
	case "/open-apis/im/v1/messages/msg-1/reply":
		body = `{"code":0,"msg":"ok","data":{}}`
	case "/open-apis/im/v1/messages":
		body = `{"code":0,"msg":"ok","data":{}}`
	case "/open-apis/im/v1/messages/msg-1":
		body = `{"code":0,"msg":"ok"}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func TestAdapterNilDataAndHelperBranches(t *testing.T) {
	client := lark.NewClient("app-id", "app-secret", lark.WithOpenBaseUrl("https://mock.feishu.test"), lark.WithHttpClient(&http.Client{Transport: adapterNilDataAPI{}}))
	a := &Adapter{client: client, reactions: map[string]string{}}

	if id, err := a.ReplyTextWithID(context.Background(), "msg-1", "hello", false); err != nil || id != "" {
		t.Fatalf("ReplyTextWithID(nil data) = %q, %v, want empty id", id, err)
	}
	if id, err := a.ReplyCard(context.Background(), "msg-1", map[string]any{"elements": []map[string]any{}}, false); err != nil || id != "" {
		t.Fatalf("ReplyCard(nil data) = %q, %v, want empty id", id, err)
	}
	if id, err := a.SendCard(context.Background(), "chat-1", map[string]any{"elements": []map[string]any{}}); err != nil || id != "" {
		t.Fatalf("SendCard(nil data) = %q, %v, want empty id", id, err)
	}
	if id, err := a.replyMessageDetailed(context.Background(), "msg-1", "text", `{"text":"hello"}`, false); err != nil || id != "" {
		t.Fatalf("replyMessageDetailed(nil data) = %q, %v, want empty id", id, err)
	}

	if _, ok := extractImageAttachment(strPtr(`{}`)); ok {
		t.Fatal("extractImageAttachment(empty) should fail")
	}
	if _, ok := extractFileAttachment(strPtr(`{}`)); ok {
		t.Fatal("extractFileAttachment(empty) should fail")
	}
	if _, ok := extractAudioAttachment(strPtr(`{}`)); ok {
		t.Fatal("extractAudioAttachment(empty) should fail")
	}
	if got := detectUploadFileType("voice.opus"); got != "opus" {
		t.Fatalf("detectUploadFileType(opus) = %q", got)
	}
	if got := sanitizeAttachmentKey(":::"); got != "___" {
		t.Fatalf("sanitizeAttachmentKey(colons) = %q", got)
	}
}
