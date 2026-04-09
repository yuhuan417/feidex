package feishu

import (
	"context"
	"errors"
	"net/http"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type adapterTransportErrorAPI struct{}

func (adapterTransportErrorAPI) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
		return (&adapterNilDataAPI{}).RoundTrip(req)
	}
	return nil, errors.New("transport failed")
}

func TestAdapterTransportErrorBranches(t *testing.T) {
	client := lark.NewClient("app-id", "app-secret", lark.WithOpenBaseUrl("https://mock.feishu.test"), lark.WithHttpClient(&http.Client{Transport: adapterTransportErrorAPI{}}))
	a := &Adapter{client: client, reactions: map[string]string{reactionKey("msg-1", "SMILE"): "reaction-1"}}

	if err := a.RemoveReaction(context.Background(), "msg-1", "SMILE"); err == nil {
		t.Fatal("expected RemoveReaction transport error")
	}
	if _, err := a.ReplyTextWithID(context.Background(), "msg-1", "hello", false); err == nil {
		t.Fatal("expected ReplyTextWithID transport error")
	}
	if err := a.SendText(context.Background(), "chat-1", "hello"); err == nil {
		t.Fatal("expected SendText transport error")
	}
	if _, err := a.ReplyCard(context.Background(), "msg-1", map[string]any{"elements": []map[string]any{}}, false); err == nil {
		t.Fatal("expected ReplyCard transport error")
	}
	if _, err := a.SendCard(context.Background(), "chat-1", map[string]any{"elements": []map[string]any{}}); err == nil {
		t.Fatal("expected SendCard transport error")
	}
	if err := a.PatchCard(context.Background(), "msg-1", map[string]any{"elements": []map[string]any{}}); err == nil {
		t.Fatal("expected PatchCard transport error")
	}
	if _, err := a.replyMessageDetailed(context.Background(), "msg-1", "text", `{"text":"hello"}`, false); err == nil {
		t.Fatal("expected replyMessageDetailed transport error")
	}
	if _, _, err := a.DownloadMessageResource(context.Background(), "msg-1", Attachment{Kind: "file", ResourceKey: "file-key"}, t.TempDir()); err == nil {
		t.Fatal("expected DownloadMessageResource transport error")
	}
}
