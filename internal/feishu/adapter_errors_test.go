package feishu

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type adapterErrorAPI struct{}

func (adapterErrorAPI) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":0,"tenant_access_token":"tenant-token","expire":7200}`)),
			Request:    req,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"code":1,"msg":"bad"}`)),
		Request:    req,
	}, nil
}

func newErrorAdapter() *Adapter {
	client := lark.NewClient("app-id", "app-secret", lark.WithOpenBaseUrl("https://mock.feishu.test"), lark.WithHttpClient(&http.Client{Transport: adapterErrorAPI{}}))
	return &Adapter{client: client, reactions: map[string]string{}}
}

func TestAdapterOutboundErrorPaths(t *testing.T) {
	a := newErrorAdapter()
	a.reactions[reactionKey("msg-1", "SMILE")] = "reaction-1"

	if err := a.RemoveReaction(context.Background(), "msg-1", "SMILE"); err == nil {
		t.Fatal("expected RemoveReaction to fail on non-success response")
	}
	if err := a.SendText(context.Background(), "chat-1", "hello"); err == nil {
		t.Fatal("expected SendText to fail on non-success response")
	}
	if _, err := a.ReplyCard(context.Background(), "msg-1", map[string]any{"elements": []map[string]any{}}, false); err == nil {
		t.Fatal("expected ReplyCard to fail on non-success response")
	}
	if _, err := a.SendCard(context.Background(), "chat-1", map[string]any{"elements": []map[string]any{}}); err == nil {
		t.Fatal("expected SendCard to fail on non-success response")
	}
	if err := a.PatchCard(context.Background(), "msg-1", map[string]any{"elements": []map[string]any{}}); err == nil {
		t.Fatal("expected PatchCard to fail on non-success response")
	}
	if _, err := a.replyMessageDetailed(context.Background(), "msg-1", "text", `{"text":"hello"}`, false); err == nil {
		t.Fatal("expected replyMessageDetailed to fail on non-success response")
	}

	if err := a.RemoveReaction(context.Background(), "missing", "SMILE"); err != nil {
		t.Fatalf("RemoveReaction(missing reaction) error = %v", err)
	}
}
