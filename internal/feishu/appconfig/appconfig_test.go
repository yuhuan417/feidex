package appconfig

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type recordedRequest struct {
	method string
	path   string
	auth   string
	body   map[string]any
}

func newMockAPI(t *testing.T, responses map[string]any) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	recorded := []recordedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		mu.Lock()
		recorded = append(recorded, recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			auth:   r.Header.Get("Authorization"),
			body:   body,
		})
		mu.Unlock()
		payload, ok := responses[r.Method+" "+r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":404,"msg":"not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch value := payload.(type) {
		case string:
			_, _ = w.Write([]byte(value))
		default:
			out, _ := json.Marshal(value)
			_, _ = w.Write(out)
		}
	}))
	t.Cleanup(server.Close)
	return server, &recorded
}

func newMockClient(server *httptest.Server) *Client {
	return &Client{
		baseURL:    server.URL,
		appID:      "cli_test",
		appSecret:  "secret",
		httpClient: server.Client(),
	}
}

func TestFetchStateReadsScopesAndPublishedVersion(t *testing.T) {
	server, recorded := newMockAPI(t, map[string]any{
		"POST /open-apis/auth/v3/tenant_access_token/internal": `{"code":0,"tenant_access_token":"tok-1"}`,
		"GET /open-apis/application/v6/applications/cli_test": `{"code":0,"data":{"app":{"scopes":[` +
			`{"scope":"im:message"},{"scope":"application:application:patch"}],"online_version_id":"oav_1"}}}`,
		"GET /open-apis/application/v6/applications/cli_test/app_versions/oav_1": `{"code":0,"data":{"app_version":{` +
			`"version":"1.0.9","scopes":[{"scope":"im:message"}],` +
			`"event_infos":[{"event_type":"im.message.receive_v1"},{"event_type":"im.message.recalled_v1"}]}}}`,
	})
	client := newMockClient(server)

	state, err := client.FetchState(context.Background())
	if err != nil {
		t.Fatalf("FetchState() error = %v", err)
	}
	if state.OnlineVersion != "1.0.9" || state.OnlineVersionID != "oav_1" {
		t.Fatalf("FetchState() version = %q/%q", state.OnlineVersion, state.OnlineVersionID)
	}
	if !state.HasScope("application:application:patch") || state.HasScope("drive:drive") {
		t.Fatalf("FetchState() app scopes = %v", state.AppScopes)
	}
	if len(state.VersionEvents) != 2 || state.VersionEvents[0] != "im.message.receive_v1" {
		t.Fatalf("FetchState() version events = %v", state.VersionEvents)
	}
	for _, req := range *recorded {
		if req.path != "/open-apis/auth/v3/tenant_access_token/internal" && req.auth != "Bearer tok-1" {
			t.Fatalf("request %s %s missing bearer token: %q", req.method, req.path, req.auth)
		}
	}
}

func TestApplyFixSendsEventAndScopeChanges(t *testing.T) {
	server, recorded := newMockAPI(t, map[string]any{
		"POST /open-apis/auth/v3/tenant_access_token/internal":         `{"code":0,"tenant_access_token":"tok-1"}`,
		"PATCH /open-apis/application/v7/applications/cli_test/config": `{"code":0,"data":{}}`,
	})
	client := newMockClient(server)

	err := client.ApplyFix(context.Background(), FixPlan{
		AddEvents:    []string{"im.message.recalled_v1"},
		RemoveEvents: []string{"im.message.message_read_v1"},
		AddScopes:    []string{"im:message.urgent"},
	})
	if err != nil {
		t.Fatalf("ApplyFix() error = %v", err)
	}
	var patch *recordedRequest
	for i := range *recorded {
		if (*recorded)[i].method == http.MethodPatch {
			patch = &(*recorded)[i]
		}
	}
	if patch == nil {
		t.Fatal("ApplyFix() did not send PATCH")
	}
	event, _ := patch.body["event"].(map[string]any)
	if event == nil || event["subscription_type"] != "websocket" {
		t.Fatalf("ApplyFix() event payload = %#v", patch.body["event"])
	}
	if got := toStrings(event["add_events"]); len(got) != 1 || got[0] != "im.message.recalled_v1" {
		t.Fatalf("ApplyFix() add_events = %#v", event["add_events"])
	}
	if got := toStrings(event["remove_events"]); len(got) != 1 || got[0] != "im.message.message_read_v1" {
		t.Fatalf("ApplyFix() remove_events = %#v", event["remove_events"])
	}
	scope, _ := patch.body["scope"].(map[string]any)
	scopes, _ := scope["add_scopes"].([]any)
	if len(scopes) != 1 {
		t.Fatalf("ApplyFix() add_scopes = %#v", scope)
	}
	item, _ := scopes[0].(map[string]any)
	if item["scope_name"] != "im:message.urgent" || item["token_type"] != "tenant" {
		t.Fatalf("ApplyFix() add_scopes item = %#v", item)
	}
}

func TestApplyFixEmptyPlanSkipsRequest(t *testing.T) {
	server, recorded := newMockAPI(t, map[string]any{})
	client := newMockClient(server)
	if err := client.ApplyFix(context.Background(), FixPlan{}); err != nil {
		t.Fatalf("ApplyFix(empty) error = %v", err)
	}
	if len(*recorded) != 0 {
		t.Fatalf("ApplyFix(empty) issued %d requests", len(*recorded))
	}
}

func TestPublishSubmitsVersion(t *testing.T) {
	server, recorded := newMockAPI(t, map[string]any{
		"POST /open-apis/auth/v3/tenant_access_token/internal":         `{"code":0,"tenant_access_token":"tok-1"}`,
		"POST /open-apis/application/v7/applications/cli_test/publish": `{"code":0,"data":{"version":"1.0.10"}}`,
	})
	client := newMockClient(server)

	version, err := client.Publish(context.Background(), "remark", "changelog")
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if version != "1.0.10" {
		t.Fatalf("Publish() version = %q", version)
	}
	var publish *recordedRequest
	for i := range *recorded {
		if strings.HasSuffix((*recorded)[i].path, "/publish") {
			publish = &(*recorded)[i]
		}
	}
	if publish == nil {
		t.Fatal("Publish() did not POST /publish")
	}
	if publish.body["remark"] != "remark" || publish.body["changelog"] != "changelog" ||
		publish.body["mobile_default_ability"] != "bot" || publish.body["pc_default_ability"] != "bot" {
		t.Fatalf("Publish() body = %#v", publish.body)
	}
}

func TestErrorClassification(t *testing.T) {
	server, _ := newMockAPI(t, map[string]any{
		"POST /open-apis/auth/v3/tenant_access_token/internal": `{"code":0,"tenant_access_token":"tok-1"}`,
		"GET /open-apis/application/v6/applications/cli_test":  `{"code":99991672,"msg":"Access denied. scopes is required: [application:application:patch]"}`,
	})
	client := newMockClient(server)
	_, err := client.FetchState(context.Background())
	if err == nil || !IsMissingScope(err) || IsUnderReview(err) {
		t.Fatalf("FetchState() error = %v (missing=%v underReview=%v)", err, IsMissingScope(err), IsUnderReview(err))
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 99991672 {
		t.Fatalf("FetchState() APIError = %#v", apiErr)
	}

	server2, _ := newMockAPI(t, map[string]any{
		"POST /open-apis/auth/v3/tenant_access_token/internal":         `{"code":0,"tenant_access_token":"tok-1"}`,
		"PATCH /open-apis/application/v7/applications/cli_test/config": `{"code":210040,"msg":"Unable to update configurations, as the app is currently under review."}`,
	})
	client2 := newMockClient(server2)
	err2 := client2.ApplyFix(context.Background(), FixPlan{AddEvents: []string{"x"}})
	if err2 == nil || !IsUnderReview(err2) || IsMissingScope(err2) {
		t.Fatalf("ApplyFix() error = %v (underReview=%v)", err2, IsUnderReview(err2))
	}
}

func TestAuthURL(t *testing.T) {
	got := AuthURL("https://open.feishu.cn/", "cli_abc", []string{"application:application:patch", "im:message.urgent"})
	want := "https://open.feishu.cn/app/cli_abc/auth?q=application:application:patch,im:message.urgent&op_from=openapi&token_type=tenant"
	if got != want {
		t.Fatalf("AuthURL() = %q, want %q", got, want)
	}
}

func TestRequiredScopesIncludePatch(t *testing.T) {
	found := false
	for _, scope := range RequiredScopes() {
		if scope == PatchScope {
			found = true
		}
	}
	if !found {
		t.Fatalf("RequiredScopes() = %v, missing %s", RequiredScopes(), PatchScope)
	}
}

func toStrings(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
