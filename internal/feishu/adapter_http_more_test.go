package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"feidex/internal/config"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type adapterMockAPI struct {
	mu       sync.Mutex
	requests []string
}

func (m *adapterMockAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, r.Method+" "+r.URL.Path)
	m.mu.Unlock()

	jsonResponse := func(status int, v any) (*http.Response, error) {
		body, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Request:    r,
		}, nil
	}
	fileResponse := func(name, contentType, body string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":        []string{contentType},
				"Content-Disposition": []string{`attachment; filename="` + name + `"`},
			},
			Body:    io.NopCloser(strings.NewReader(body)),
			Request: r,
		}, nil
	}

	switch r.URL.Path {
	case "/open-apis/auth/v3/tenant_access_token/internal":
		return jsonResponse(http.StatusOK, map[string]any{
			"code":                0,
			"msg":                 "ok",
			"tenant_access_token": "tenant-token",
			"expire":              7200,
		})
	case "/open-apis/im/v1/messages/msg-1/reactions":
		return jsonResponse(http.StatusOK, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{"reaction_id": "reaction-1"},
		})
	case "/open-apis/im/v1/messages/msg-1/reactions/reaction-1":
		return jsonResponse(http.StatusOK, map[string]any{"code": 0, "msg": "ok"})
	case "/open-apis/im/v1/messages/msg-1/reply":
		return jsonResponse(http.StatusOK, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{"message_id": "reply-1"},
		})
	case "/open-apis/im/v1/messages":
		return jsonResponse(http.StatusOK, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{"message_id": "msg-1"},
		})
	case "/open-apis/im/v1/images":
		return jsonResponse(http.StatusOK, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{"image_key": "image-key"},
		})
	case "/open-apis/im/v1/files":
		return jsonResponse(http.StatusOK, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{"file_key": "file-key"},
		})
	case "/open-apis/im/v1/messages/msg-2":
		return jsonResponse(http.StatusOK, map[string]any{"code": 0, "msg": "ok"})
	case "/open-apis/im/v1/messages/msg-mf-text":
		return jsonResponse(http.StatusOK, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{
				"items": []map[string]any{{
					"message_id": "msg-mf-text",
					"msg_type":   "text",
					"body": map[string]any{
						"content": `{"text":"forwarded hello"}`,
					},
				}},
			},
		})
	case "/open-apis/im/v1/messages/msg-mf-post":
		return jsonResponse(http.StatusOK, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{
				"items": []map[string]any{{
					"message_id": "msg-mf-post",
					"msg_type":   "post",
					"body": map[string]any{
						"content": `{"title":"Title","content":[[{"tag":"text","text":"caption"}],[{"tag":"img","image_key":"img-post-forward"}]]}`,
					},
				}},
			},
		})
	case "/open-apis/im/v1/messages/msg-mf-image":
		return jsonResponse(http.StatusOK, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{
				"items": []map[string]any{{
					"message_id": "msg-mf-image",
					"msg_type":   "image",
					"body": map[string]any{
						"content": `{"image_key":"img-forward"}`,
					},
				}},
			},
		})
	case "/open-apis/im/v1/messages/msg-mf-nested":
		return jsonResponse(http.StatusOK, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{
				"items": []map[string]any{{
					"message_id": "msg-mf-nested",
					"msg_type":   "merge_forward",
					"body": map[string]any{
						"content": `{"message_id_list":["msg-mf-post"]}`,
					},
				}},
			},
		})
	case "/open-apis/im/v1/messages/msg-1/resources/file-key":
		return fileResponse("report.txt", "application/octet-stream", "report")
	default:
		return jsonResponse(http.StatusNotFound, map[string]any{"code": 404, "msg": "not found"})
	}
}

type adapterRoundTripper struct {
	api *adapterMockAPI
}

func (r adapterRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.api.RoundTrip(req)
}

func (m *adapterMockAPI) count(path string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, req := range m.requests {
		if strings.Contains(req, path) {
			count++
		}
	}
	return count
}

func newHTTPBackedAdapter(t *testing.T) (*Adapter, *adapterMockAPI) {
	t.Helper()

	mock := &adapterMockAPI{}
	cfg := config.FeishuConfig{AppID: "app-id", AppSecret: "app-secret"}
	factory := func() *lark.Client {
		return lark.NewClient(
			cfg.AppID,
			cfg.AppSecret,
			lark.WithOpenBaseUrl("https://mock.feishu.test"),
			lark.WithEnableTokenCache(true),
			lark.WithTokenCache(sharedFeishuTokenCache),
			lark.WithHttpClient(&http.Client{Transport: adapterRoundTripper{api: mock}}),
		)
	}
	sharedFeishuTokenCache.clearTenantAccessTokens(cfg.AppID)
	return &Adapter{
		cfg:           cfg,
		client:        factory(),
		clientFactory: factory,
		allowAll:      true,
		seen:          map[string]time.Time{},
		reactions:     map[string]string{},
	}, mock
}

type tokenRefreshMockAPI struct {
	mu            sync.Mutex
	authCount     int
	invalidByPath map[string]string
	pathCount     map[string]int
}

func (m *tokenRefreshMockAPI) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.invalidByPath == nil {
		m.invalidByPath = map[string]string{}
	}
	if m.pathCount == nil {
		m.pathCount = map[string]int{}
	}
	m.pathCount[req.URL.Path]++

	jsonResponse := func(status int, body string) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}

	switch req.URL.Path {
	case "/open-apis/auth/v3/tenant_access_token/internal":
		m.authCount++
		return jsonResponse(http.StatusOK, fmt.Sprintf(`{"code":0,"tenant_access_token":"tenant-token-%d","expire":7200}`, m.authCount))
	case "/open-apis/im/v1/messages/msg-retry/reply":
		if m.shouldRejectLocked(req) {
			return jsonResponse(http.StatusOK, `{"code":99991663,"msg":"Invalid access token for authorization. Please make a request with token attached."}`)
		}
		return jsonResponse(http.StatusOK, `{"code":0,"msg":"ok","data":{"message_id":"reply-retry"}}`)
	case "/open-apis/im/v1/messages/msg-retry/reactions":
		if m.shouldRejectLocked(req) {
			return jsonResponse(http.StatusOK, `{"code":99991663,"msg":"Invalid access token for authorization. Please make a request with token attached."}`)
		}
		return jsonResponse(http.StatusOK, `{"code":0,"msg":"ok","data":{"reaction_id":"reaction-retry"}}`)
	default:
		return jsonResponse(http.StatusNotFound, `{"code":404,"msg":"not found"}`)
	}
}

func (m *tokenRefreshMockAPI) shouldRejectLocked(req *http.Request) bool {
	auth := strings.TrimSpace(req.Header.Get("Authorization"))
	if auth == "" {
		return true
	}
	if m.invalidByPath[req.URL.Path] == "" {
		m.invalidByPath[req.URL.Path] = auth
	}
	return m.invalidByPath[req.URL.Path] == auth
}

func (m *tokenRefreshMockAPI) count(path string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pathCount[path]
}

func newTokenRefreshAdapter(t *testing.T) (*Adapter, *tokenRefreshMockAPI) {
	t.Helper()
	mock := &tokenRefreshMockAPI{}
	cfg := config.FeishuConfig{AppID: "retry-app", AppSecret: "retry-secret"}
	factory := func() *lark.Client {
		return lark.NewClient(
			cfg.AppID,
			cfg.AppSecret,
			lark.WithOpenBaseUrl("https://mock.feishu.test"),
			lark.WithEnableTokenCache(true),
			lark.WithTokenCache(sharedFeishuTokenCache),
			lark.WithHttpClient(&http.Client{Transport: roundTripperFunc(mock.RoundTrip)}),
		)
	}
	sharedFeishuTokenCache.clearTenantAccessTokens(cfg.AppID)
	return &Adapter{
		cfg:           cfg,
		client:        factory(),
		clientFactory: factory,
		allowAll:      true,
		seen:          map[string]time.Time{},
		reactions:     map[string]string{},
	}, mock
}

func TestAdapterRefreshesTenantTokenForReplyAndReaction(t *testing.T) {
	a, mock := newTokenRefreshAdapter(t)
	card := map[string]any{"elements": []map[string]any{{"tag": "markdown", "content": "body"}}}

	id, err := a.ReplyCard(context.Background(), "msg-retry", card, true)
	if err != nil {
		t.Fatalf("ReplyCard() error = %v", err)
	}
	if id != "reply-retry" {
		t.Fatalf("ReplyCard() id = %q, want reply-retry", id)
	}
	if got := mock.count("/open-apis/im/v1/messages/msg-retry/reply"); got < 2 {
		t.Fatalf("reply requests = %d, want retry after invalid token", got)
	}

	if err := a.AddReaction(context.Background(), "msg-retry", "SMILE"); err != nil {
		t.Fatalf("AddReaction() error = %v", err)
	}
	if got := a.reactions[reactionKey("msg-retry", "SMILE")]; got != "reaction-retry" {
		t.Fatalf("stored reaction id = %q, want reaction-retry", got)
	}
	if got := mock.count("/open-apis/im/v1/messages/msg-retry/reactions"); got < 2 {
		t.Fatalf("reaction requests = %d, want retry after invalid token", got)
	}

	mock.mu.Lock()
	authCount := mock.authCount
	mock.mu.Unlock()
	if authCount < 3 {
		t.Fatalf("tenant token fetches = %d, want refreshes for reply and reaction", authCount)
	}
}

func TestAdapterOutboundMethodsAgainstHTTPAPI(t *testing.T) {
	a, mock := newHTTPBackedAdapter(t)

	if err := a.AddReaction(context.Background(), "", ""); err != nil {
		t.Fatalf("AddReaction(empty) error = %v", err)
	}
	if err := a.AddReaction(context.Background(), "msg-1", "SMILE"); err != nil {
		t.Fatalf("AddReaction() error = %v", err)
	}
	if got := a.reactions[reactionKey("msg-1", "SMILE")]; got != "reaction-1" {
		t.Fatalf("stored reaction id = %q, want reaction-1", got)
	}
	if err := a.AddReaction(context.Background(), "msg-1", "SMILE"); err != nil {
		t.Fatalf("AddReaction(duplicate) error = %v", err)
	}
	if mock.count("/open-apis/im/v1/messages/msg-1/reactions") != 1 {
		t.Fatalf("reaction create requests = %d, want 1", mock.count("/open-apis/im/v1/messages/msg-1/reactions"))
	}
	if err := a.RemoveReaction(context.Background(), "msg-1", "SMILE"); err != nil {
		t.Fatalf("RemoveReaction() error = %v", err)
	}
	if _, ok := a.reactions[reactionKey("msg-1", "SMILE")]; ok {
		t.Fatalf("reaction key still present after remove: %+v", a.reactions)
	}

	if err := a.ReplyText(context.Background(), "msg-1", "hello", true); err != nil {
		t.Fatalf("ReplyText() error = %v", err)
	}
	if id, err := a.ReplyTextWithID(context.Background(), "msg-1", "hello", false); err != nil || id != "reply-1" {
		t.Fatalf("ReplyTextWithID() = %q, %v, want reply-1", id, err)
	}
	if err := a.SendText(context.Background(), "chat-1", "hello"); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}

	card := map[string]any{"elements": []map[string]any{{"tag": "markdown", "content": "body"}}}
	if id, err := a.ReplyCard(context.Background(), "msg-1", card, true); err != nil || id != "reply-1" {
		t.Fatalf("ReplyCard() = %q, %v, want reply-1", id, err)
	}
	if id, err := a.SendCard(context.Background(), "chat-1", card); err != nil || id != "msg-1" {
		t.Fatalf("SendCard() = %q, %v, want msg-1", id, err)
	}
	if err := a.PatchCard(context.Background(), "msg-2", card); err != nil {
		t.Fatalf("PatchCard() error = %v", err)
	}

	dir := t.TempDir()
	path, name, err := a.DownloadMessageResource(context.Background(), "msg-1", Attachment{Kind: "file", ResourceKey: "file-key"}, dir)
	if err != nil {
		t.Fatalf("DownloadMessageResource() error = %v", err)
	}
	if name != "report.txt" {
		t.Fatalf("downloaded name = %q, want report.txt", name)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile(downloaded) error = %v", readErr)
	}
	if string(content) != "report" {
		t.Fatalf("downloaded content = %q, want report", string(content))
	}
}

func TestAdapterLookupMessageSenderAndUrgentAppUseOpenID(t *testing.T) {
	var (
		lookupUserIDType string
		urgentUserIDType string
	)
	client := lark.NewClient(
		"app-id",
		"app-secret",
		lark.WithOpenBaseUrl("https://mock.feishu.test"),
		lark.WithEnableTokenCache(true),
		lark.WithHttpClient(&http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			jsonResponse := func(status int, body string) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(body)),
					Request:    req,
				}, nil
			}
			switch req.URL.Path {
			case "/open-apis/auth/v3/tenant_access_token/internal":
				return jsonResponse(http.StatusOK, `{"code":0,"tenant_access_token":"tenant-token","expire":7200}`)
			case "/open-apis/im/v1/messages/msg-lookup":
				lookupUserIDType = req.URL.Query().Get("user_id_type")
				return jsonResponse(http.StatusOK, `{"code":0,"msg":"ok","data":{"items":[{"message_id":"msg-lookup","sender":{"id":"ou_lookup","id_type":"open_id","sender_type":"user"}}]}}`)
			case "/open-apis/im/v1/messages/msg-urgent/urgent_app":
				urgentUserIDType = req.URL.Query().Get("user_id_type")
				return jsonResponse(http.StatusOK, `{"code":0,"msg":"ok","data":{}}`)
			default:
				return jsonResponse(http.StatusNotFound, `{"code":404,"msg":"not found"}`)
			}
		})}),
	)
	a := &Adapter{
		client:    client,
		allowAll:  true,
		seen:      map[string]time.Time{},
		reactions: map[string]string{},
	}

	userID, err := a.LookupMessageSenderOpenID(context.Background(), "msg-lookup")
	if err != nil {
		t.Fatalf("LookupMessageSenderOpenID() error = %v", err)
	}
	if userID != "ou_lookup" {
		t.Fatalf("LookupMessageSenderOpenID() = %q, want ou_lookup", userID)
	}
	if lookupUserIDType != larkim.UserIdTypeGetMessageOpenId {
		t.Fatalf("LookupMessageSenderOpenID() user_id_type = %q, want %q", lookupUserIDType, larkim.UserIdTypeGetMessageOpenId)
	}

	if err := a.UrgentApp(context.Background(), "msg-urgent", "ou_lookup"); err != nil {
		t.Fatalf("UrgentApp() error = %v", err)
	}
	if urgentUserIDType != larkim.UserIdTypeUrgentAppMessageOpenId {
		t.Fatalf("UrgentApp() user_id_type = %q, want %q", urgentUserIDType, larkim.UserIdTypeUrgentAppMessageOpenId)
	}
}

func TestAdapterFileAndDownloadValidationErrors(t *testing.T) {
	a, _ := newHTTPBackedAdapter(t)

	if err := a.ReplyLocalFile(context.Background(), "msg-1", filepath.Join(t.TempDir(), "missing.txt"), false); err == nil {
		t.Fatal("expected missing file to fail")
	}

	dir := t.TempDir()
	if err := a.ReplyLocalFile(context.Background(), "msg-1", dir, false); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("ReplyLocalFile(dir) error = %v, want regular file error", err)
	}

	empty := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("WriteFile(empty) error = %v", err)
	}
	if err := a.ReplyLocalFile(context.Background(), "msg-1", empty, false); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("ReplyLocalFile(empty) error = %v, want empty file error", err)
	}

	large := filepath.Join(t.TempDir(), "large.bin")
	f, err := os.Create(large)
	if err != nil {
		t.Fatalf("Create(large) error = %v", err)
	}
	if err := f.Truncate(31 * 1024 * 1024); err != nil {
		t.Fatalf("Truncate(large) error = %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close(large) error = %v", err)
	}
	info, err := os.Stat(large)
	if err != nil {
		t.Fatalf("Stat(large) error = %v", err)
	}
	if err := a.replyLocalUploadedFile(context.Background(), "msg-1", large, info, "file", false); err == nil || !strings.Contains(err.Error(), "30MB") {
		t.Fatalf("replyLocalUploadedFile(large) error = %v, want size limit", err)
	}

	if _, _, err := a.DownloadMessageResource(context.Background(), "", Attachment{Kind: "file", ResourceKey: "x"}, t.TempDir()); err == nil {
		t.Fatal("expected missing message id to fail")
	}
	if _, _, err := a.DownloadMessageResource(context.Background(), "msg-1", Attachment{Kind: "bad", ResourceKey: "x"}, t.TempDir()); err == nil {
		t.Fatal("expected unsupported kind to fail")
	}
	if _, _, err := a.DownloadMessageResource(context.Background(), "msg-1", Attachment{Kind: "file"}, t.TempDir()); err == nil {
		t.Fatal("expected missing resource key to fail")
	}
}

func TestAdapterReplyLocalFileSuccessPaths(t *testing.T) {
	a, _ := newHTTPBackedAdapter(t)

	image := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(image, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile(image) error = %v", err)
	}
	if err := a.ReplyLocalFile(context.Background(), "msg-1", image, true); err != nil {
		t.Fatalf("ReplyLocalFile(image) error = %v", err)
	}

	filePath := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(file) error = %v", err)
	}
	if err := a.ReplyLocalFile(context.Background(), "msg-1", filePath, false); err != nil {
		t.Fatalf("ReplyLocalFile(file) error = %v", err)
	}
}

func TestConvertMessageMergeForwardKeepsDeferredIDs(t *testing.T) {
	a, _ := newHTTPBackedAdapter(t)
	userID := "user-1"
	chatType := "p2p"
	msgType := "merge_forward"
	content := `{"message_id_list":["msg-mf-text","msg-mf-nested","msg-mf-image"]}`

	msg := a.convertMessage(&larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: &userID}},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("msg-merge-root"),
				ChatType:    &chatType,
				MessageType: &msgType,
				Content:     &content,
			},
		},
	})
	if msg == nil {
		t.Fatal("convertMessage(merge_forward) returned nil")
	}
	if msg.Text != "" {
		t.Fatalf("convertMessage(merge_forward) text = %q, want empty before prefetch", msg.Text)
	}
	if len(msg.Attachments) != 0 {
		t.Fatalf("convertMessage(merge_forward) attachments = %+v, want deferred empty attachments", msg.Attachments)
	}
	if len(msg.MergeForwardMessageIDs) != 3 || msg.MergeForwardMessageIDs[0] != "msg-mf-text" || msg.MergeForwardMessageIDs[2] != "msg-mf-image" {
		t.Fatalf("convertMessage(merge_forward) ids = %+v", msg.MergeForwardMessageIDs)
	}
}

func TestResolveMergeForwardExpandsForwardedMessages(t *testing.T) {
	a, _ := newHTTPBackedAdapter(t)
	text, attachments, err := a.ResolveMergeForward(context.Background(), "msg-merge-root", []string{"msg-mf-text", "msg-mf-nested", "msg-mf-image"})
	if err != nil {
		t.Fatalf("ResolveMergeForward() error = %v", err)
	}
	if text != "forwarded hello\n\nTitle\ncaption" {
		t.Fatalf("ResolveMergeForward() text = %q", text)
	}
	if len(attachments) != 2 {
		t.Fatalf("ResolveMergeForward() attachments = %+v, want 2", attachments)
	}
	if attachments[0].ResourceKey != "img-post-forward" || attachments[0].SourceMessageID != "msg-mf-post" {
		t.Fatalf("first forwarded attachment = %+v, want post image source", attachments[0])
	}
	if attachments[1].ResourceKey != "img-forward" || attachments[1].SourceMessageID != "msg-mf-image" {
		t.Fatalf("second forwarded attachment = %+v, want image source", attachments[1])
	}
}

func TestFetchBotOpenIDSuccess(t *testing.T) {
	origTransport := http.DefaultTransport
	http.DefaultTransport = adapterRoundTripper{api: &adapterMockAPI{}}
	defer func() { http.DefaultTransport = origTransport }()

	api := &adapterMockAPI{}
	http.DefaultTransport = adapterRoundTripper{api: api}
	if got := (&Adapter{cfg: config.FeishuConfig{AppID: "app", AppSecret: "secret"}}).fetchBotOpenID(); got != "" {
		t.Fatalf("fetchBotOpenID(unhandled) = %q, want empty without bot info endpoint", got)
	}

	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			body := `{"code":0,"tenant_access_token":"tenant-token"}`
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		case "/open-apis/bot/v3/info":
			body := `{"code":0,"bot":{"open_id":"ou_bot","app_name":"luban-feidex"}}`
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":404}`)), Request: req}, nil
		}
	})
	if got := (&Adapter{cfg: config.FeishuConfig{AppID: "app", AppSecret: "secret"}}).fetchBotOpenID(); got != "ou_bot" {
		t.Fatalf("fetchBotOpenID() = %q, want ou_bot", got)
	}
	profile := (&Adapter{cfg: config.FeishuConfig{AppID: "app", AppSecret: "secret"}}).fetchBotProfile()
	if profile.OpenID != "ou_bot" || profile.Name != "luban-feidex" {
		t.Fatalf("fetchBotProfile() = %+v, want open id and app name", profile)
	}
}
