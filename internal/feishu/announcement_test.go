package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"feidex/internal/config"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

type announcementMockAPI struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string]string
}

func (m *announcementMockAPI) RoundTrip(req *http.Request) (*http.Response, error) {
	bodyBytes, _ := io.ReadAll(req.Body)
	m.mu.Lock()
	if m.bodies == nil {
		m.bodies = map[string]string{}
	}
	m.requests = append(m.requests, req.Method+" "+req.URL.Path)
	m.bodies[req.URL.Path] = string(bodyBytes)
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
			Request:    req,
		}, nil
	}

	switch req.URL.Path {
	case "/open-apis/auth/v3/tenant_access_token/internal":
		return jsonResponse(http.StatusOK, map[string]any{
			"code":                0,
			"msg":                 "ok",
			"tenant_access_token": "tenant-token",
			"expire":              7200,
		})
	case "/open-apis/docx/v1/chats/chat-1/announcement/blocks":
		return jsonResponse(http.StatusOK, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{
				"items": []map[string]any{{
					"block_id":   "block-existing",
					"block_type": 2,
					"text": map[string]any{"elements": []map[string]any{{
						"text_run": map[string]any{"content": "marker text"},
					}}},
				}},
			},
		})
	case "/open-apis/docx/v1/chats/chat-1/announcement/blocks/chat-1/children":
		return jsonResponse(http.StatusOK, map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{
				"children": []map[string]any{{
					"block_id":   "block-created",
					"block_type": 2,
					"text": map[string]any{"elements": []map[string]any{{
						"text_run": map[string]any{"content": "created text"},
					}}},
				}},
			},
		})
	case "/open-apis/docx/v1/chats/chat-1/announcement/blocks/batch_update":
		return jsonResponse(http.StatusOK, map[string]any{"code": 0, "msg": "ok", "data": map[string]any{}})
	default:
		return jsonResponse(http.StatusNotFound, map[string]any{"code": 404, "msg": "not found"})
	}
}

func (m *announcementMockAPI) count(path string) int {
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

func (m *announcementMockAPI) body(path string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.bodies[path]
}

func newAnnouncementHTTPAdapter(t *testing.T) (*Adapter, *announcementMockAPI) {
	t.Helper()
	mock := &announcementMockAPI{}
	cfg := config.FeishuConfig{AppID: "announcement-app", AppSecret: "announcement-secret"}
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
		cfg:               cfg,
		client:            factory(),
		clientFactory:     factory,
		allowAll:          true,
		seen:              map[string]time.Time{},
		announcementPacer: newRequestPacer(0),
	}, mock
}

func TestAdapterAnnouncementMethodsAgainstHTTPAPI(t *testing.T) {
	a, mock := newAnnouncementHTTPAdapter(t)

	blocks, err := a.ListAnnouncementBlocks(context.Background(), "chat-1")
	if err != nil {
		t.Fatalf("ListAnnouncementBlocks() error = %v", err)
	}
	if len(blocks) != 1 || blocks[0].BlockID != "block-existing" || blocks[0].Text != "marker text" {
		t.Fatalf("ListAnnouncementBlocks() = %+v", blocks)
	}

	created, err := a.CreateAnnouncementTextBlock(context.Background(), "chat-1", "chat-1", "created text", "create-token")
	if err != nil {
		t.Fatalf("CreateAnnouncementTextBlock() error = %v", err)
	}
	if created.BlockID != "block-created" || created.Text != "created text" {
		t.Fatalf("CreateAnnouncementTextBlock() = %+v", created)
	}

	if err := a.UpdateAnnouncementTextBlock(context.Background(), "chat-1", "block-created", "updated text", "update-token"); err != nil {
		t.Fatalf("UpdateAnnouncementTextBlock() error = %v", err)
	}

	if got := mock.count("/open-apis/docx/v1/chats/chat-1/announcement/blocks"); got != 3 {
		t.Fatalf("announcement endpoint requests = %d, want 3", got)
	}
	if body := mock.body("/open-apis/docx/v1/chats/chat-1/announcement/blocks/chat-1/children"); !strings.Contains(body, "created text") {
		t.Fatalf("create body missing content: %s", body)
	}
	if body := mock.body("/open-apis/docx/v1/chats/chat-1/announcement/blocks/batch_update"); !strings.Contains(body, "updated text") || !strings.Contains(body, "block-created") {
		t.Fatalf("update body missing content/block id: %s", body)
	}
}

func TestIsAnnouncementRateLimit(t *testing.T) {
	if !IsAnnouncementRateLimit(&AnnouncementAPIError{HTTPStatus: http.StatusTooManyRequests}) {
		t.Fatal("HTTP 429 should be classified as announcement rate limit")
	}
	if !IsAnnouncementRateLimit(&AnnouncementAPIError{Code: announcementRateLimitCode}) {
		t.Fatal("Feishu announcement rate-limit code should be classified")
	}
	if !IsAnnouncementRateLimit(&larkcore.CodeError{Code: announcementRateLimitCode}) {
		t.Fatal("SDK CodeError rate-limit code should be classified")
	}
	if IsAnnouncementRateLimit(errors.New("boom")) {
		t.Fatal("generic error should not be classified as announcement rate limit")
	}
}
