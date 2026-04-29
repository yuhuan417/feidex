package servicetiercmd

import (
	"strings"
	"sync"
	"testing"

	"feidex/internal/app/appcore"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type testApp struct {
	cfg       *config.Config
	configMu  sync.RWMutex
	store     *state.Store
	frontend  string
	feishuCli appcore.FeishuClient
}

func (a *testApp) Config() *config.Config                { return a.cfg }
func (a *testApp) ConfigMu() *sync.RWMutex               { return &a.configMu }
func (a *testApp) Backend() string                       { return "" }
func (a *testApp) FrontendID() string                    { return a.frontend }
func (a *testApp) FrontendConfigIndex() int              { return -1 }
func (a *testApp) Store() *state.Store                   { return a.store }
func (a *testApp) Feishu() appcore.FeishuClient          { return a.feishuCli }
func (a *testApp) ServiceTierAppState() AppStateProvider { return appStateProvider{store: a.store} }
func (a *testApp) MenuCardBody(action, body string) string {
	return body
}

type appStateProvider struct {
	store *state.Store
}

func (p appStateProvider) Session(key string) *state.Session {
	if p.store == nil {
		return nil
	}
	return p.store.GetSession(key)
}

func (p appStateProvider) SaveSession(sess *state.Session) error {
	if p.store == nil || sess == nil {
		return nil
	}
	return p.store.UpsertSession(sess)
}

func TestServiceTierHelpersAndMenu(t *testing.T) {
	if got := NormalizeServiceTier(" FAST "); got != ServiceTierFast {
		t.Fatalf("NormalizeServiceTier() = %q, want fast", got)
	}
	if got := NormalizeServiceTier("safe"); got != "" {
		t.Fatalf("NormalizeServiceTier(unsupported) = %q, want empty", got)
	}
	if got := ToggleServiceTier(""); got != ServiceTierFast {
		t.Fatalf("ToggleServiceTier(empty) = %q, want fast", got)
	}
	if got := ToggleServiceTier(ServiceTierFast); got != "" {
		t.Fatalf("ToggleServiceTier(fast) = %q, want empty", got)
	}
	if got := RenderServiceTierValue(""); got != "-" {
		t.Fatalf("RenderServiceTierValue(empty) = %q, want -", got)
	}
	if got := RenderServiceTierReplyValue(ServiceTierFast); got != "`fast`" {
		t.Fatalf("RenderServiceTierReplyValue(fast) = %q", got)
	}

	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	cfg := config.Default()
	svc := NewService(&testApp{
		cfg:       cfg,
		store:     store,
		feishuCli: feishu.New(cfg.Feishu),
	})

	card := svc.RenderMenuCard("sess-1")
	if body := cardElementsForTest(card)[0]["content"].(string); !strings.Contains(body, "当前没有活动线程") {
		t.Fatalf("RenderMenuCard(no thread) = %q", body)
	}

	if err := store.UpsertSession(&state.Session{
		Key:                     "sess-1",
		WorkspaceID:             "default",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "default",
		ActiveThreadServiceTier: ServiceTierFast,
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	card = svc.RenderMenuCard("sess-1")
	rows := cardElementsForTest(card)
	var primaryFound bool
	for _, row := range rows[1:] {
		columns, _ := row["columns"].([]map[string]any)
		for _, column := range columns {
			elements, _ := column["elements"].([]map[string]any)
			for _, elem := range elements {
				if tag, _ := elem["tag"].(string); tag == "button" && elem["type"] == "primary" {
					primaryFound = true
				}
			}
		}
	}
	if !primaryFound {
		t.Fatalf("service tier rows = %#v, want selected primary button", rows)
	}
}

func TestSetThreadServiceTierAndCommandFastValidation(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	cfg := config.Default()
	svc := NewService(&testApp{
		cfg:       cfg,
		store:     store,
		feishuCli: feishu.New(cfg.Feishu),
	})

	if _, err := svc.SetThreadServiceTier("sess-1", "thread-1", ServiceTierFast); err == nil || !strings.Contains(err.Error(), "没有活动线程") {
		t.Fatalf("SetThreadServiceTier(no thread) error = %v", err)
	}
	if err := store.UpsertSession(&state.Session{
		Key:            "sess-1",
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := svc.SetThreadServiceTier("sess-1", "thread-x", ServiceTierFast); err == nil || !strings.Contains(err.Error(), "已失效") {
		t.Fatalf("SetThreadServiceTier(stale) error = %v", err)
	}
	sess, err := svc.SetThreadServiceTier("sess-1", "thread-1", ServiceTierFast)
	if err != nil || sess.ActiveThreadServiceTier != ServiceTierFast {
		t.Fatalf("SetThreadServiceTier() = %+v, %v", sess, err)
	}

	if err := svc.CommandFast(nil, []string{"extra"}); err == nil {
		t.Fatal("expected CommandFast(args) to fail")
	}
	if err := svc.CommandFast(nil, nil); err != nil {
		t.Fatalf("CommandFast(nil msg) error = %v", err)
	}
}

func cardElementsForTest(card map[string]any) []map[string]any {
	if elements, ok := card["elements"].([]map[string]any); ok {
		return elements
	}
	body, _ := card["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	return elements
}
