package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/feishu/appconfig"
)

type fakeAppConfigHealClient struct {
	state      *appconfig.State
	stateAfter *appconfig.State
	fetchCalls int
	fixes      []appconfig.FixPlan
	publishes  []string
	applyErr   error
	publishErr error
}

func (f *fakeAppConfigHealClient) FetchState(context.Context) (*appconfig.State, error) {
	f.fetchCalls++
	if f.fetchCalls > 1 && f.stateAfter != nil {
		return f.stateAfter, nil
	}
	return f.state, nil
}

func (f *fakeAppConfigHealClient) ApplyFix(_ context.Context, plan appconfig.FixPlan) error {
	f.fixes = append(f.fixes, plan)
	return f.applyErr
}

func (f *fakeAppConfigHealClient) Publish(_ context.Context, remark, changelog string) (string, error) {
	f.publishes = append(f.publishes, remark+"|"+changelog)
	return "1.0.10", f.publishErr
}

func withFakeHealClient(t *testing.T, fake appConfigHealClient) {
	t.Helper()
	restore := newAppConfigHealClient
	newAppConfigHealClient = func(*config.FeishuConfig) appConfigHealClient { return fake }
	t.Cleanup(func() { newAppConfigHealClient = restore })
}

func inSyncHealState() *appconfig.State {
	scopes := append([]string{}, appconfig.RequiredScopes()...)
	events := append([]string{}, feishu.RequiredEventTypes()...)
	return &appconfig.State{
		AppScopes:       scopes,
		OnlineVersion:   "1.0.0",
		OnlineVersionID: "oav_1",
		VersionScopes:   scopes,
		VersionEvents:   events,
	}
}

func TestFeishuAppConfigHealInSyncIsSilent(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.AppID = "cli_test"
	fake := &fakeAppConfigHealClient{state: inSyncHealState()}
	withFakeHealClient(t, fake)

	runFeishuAppConfigHeal(a)

	if fake.fetchCalls != 1 || len(fake.fixes) != 0 || len(fake.publishes) != 0 {
		t.Fatalf("in-sync heal made changes: fetch=%d fixes=%d publishes=%d",
			fake.fetchCalls, len(fake.fixes), len(fake.publishes))
	}
	if notes := a.State().FrontendCardNotifications(); len(notes) != 0 {
		t.Fatalf("in-sync heal queued notifications: %+v", notes)
	}
}

func TestFeishuAppConfigHealRequestsAuthorizationWithoutPatch(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.AppID = "cli_test"
	state := inSyncHealState()
	state.AppScopes = []string{"im:message"}
	state.VersionScopes = []string{"im:message"}
	fake := &fakeAppConfigHealClient{state: state}
	withFakeHealClient(t, fake)

	runFeishuAppConfigHeal(a)

	if len(fake.fixes) != 0 || len(fake.publishes) != 0 {
		t.Fatalf("heal without patch scope attempted changes: fixes=%d publishes=%d", len(fake.fixes), len(fake.publishes))
	}
	notes := a.State().FrontendCardNotifications()
	if len(notes) != 1 {
		t.Fatalf("expected one queued authorization card, got %+v", notes)
	}
	note := notes[0]
	if note.Kind != feishuAppConfigHealKind || note.Title != "需要飞书授权" {
		t.Fatalf("authorization card = %+v", note)
	}
	if !strings.Contains(note.Body, "/app/cli_test/auth?q=") ||
		!strings.Contains(note.Body, appconfig.PatchScope) ||
		!strings.Contains(note.Body, "im:message.urgent") {
		t.Fatalf("authorization card body = %q", note.Body)
	}
}

func TestFeishuAppConfigHealRepairsDriftAndReports(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.AppID = "cli_test"
	drifted := inSyncHealState()
	drifted.VersionScopes = dropString(drifted.VersionScopes, "drive:drive")
	drifted.VersionEvents = append(drifted.VersionEvents, "im.message.message_read_v1")
	fake := &fakeAppConfigHealClient{state: drifted, stateAfter: inSyncHealState()}
	withFakeHealClient(t, fake)

	runFeishuAppConfigHeal(a)

	if len(fake.fixes) != 1 {
		t.Fatalf("expected one fix, got %+v", fake.fixes)
	}
	fix := fake.fixes[0]
	if len(fix.AddScopes) != 1 || fix.AddScopes[0] != "drive:drive" {
		t.Fatalf("fix.AddScopes = %v", fix.AddScopes)
	}
	if len(fix.RemoveEvents) != 1 || fix.RemoveEvents[0] != "im.message.message_read_v1" {
		t.Fatalf("fix.RemoveEvents = %v", fix.RemoveEvents)
	}
	if len(fix.AddEvents) != 0 {
		t.Fatalf("fix.AddEvents = %v", fix.AddEvents)
	}
	if len(fake.publishes) != 1 {
		t.Fatalf("expected one publish, got %v", fake.publishes)
	}
	notes := a.State().FrontendCardNotifications()
	if len(notes) != 1 || notes[0].Title != "飞书配置已自动修复" {
		t.Fatalf("success card = %+v", notes)
	}
	if !strings.Contains(notes[0].Body, "1.0.10") || !strings.Contains(notes[0].Body, "drive:drive") {
		t.Fatalf("success card body = %q", notes[0].Body)
	}
}

func TestFeishuAppConfigHealReportsPendingPublish(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.AppID = "cli_test"
	drifted := inSyncHealState()
	drifted.VersionScopes = dropString(drifted.VersionScopes, "drive:drive")
	fake := &fakeAppConfigHealClient{state: drifted, stateAfter: drifted}
	withFakeHealClient(t, fake)

	runFeishuAppConfigHeal(a)

	notes := a.State().FrontendCardNotifications()
	if len(notes) != 1 || notes[0].Title != "飞书配置修复已提交,等待生效" {
		t.Fatalf("pending card = %+v", notes)
	}
}

func TestFeishuAppConfigHealSkipsWithoutAppID(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.AppID = ""
	fake := &fakeAppConfigHealClient{state: inSyncHealState()}
	withFakeHealClient(t, fake)

	runFeishuAppConfigHeal(a)

	if fake.fetchCalls != 0 {
		t.Fatalf("heal ran without app id: fetch=%d", fake.fetchCalls)
	}
}

func TestBuildAppConfigHealPlan(t *testing.T) {
	plan := buildAppConfigHealPlan(&appconfig.State{
		VersionScopes: []string{"im:message"},
		VersionEvents: []string{"im.message.receive_v1", "im.message.message_read_v1"},
	})
	if !slices.Contains(plan.AddScopes, "drive:drive") || !slices.Contains(plan.AddScopes, appconfig.PatchScope) {
		t.Fatalf("plan.AddScopes = %v", plan.AddScopes)
	}
	if !slices.Contains(plan.AddEvents, "im.message.recalled_v1") || !slices.Contains(plan.AddEvents, "im.chat.member.bot.added_v1") {
		t.Fatalf("plan.AddEvents = %v", plan.AddEvents)
	}
	if slices.Contains(plan.AddEvents, "im.message.receive_v1") {
		t.Fatalf("plan.AddEvents must not re-add present events: %v", plan.AddEvents)
	}
	if len(plan.RemoveEvents) != 1 || plan.RemoveEvents[0] != "im.message.message_read_v1" {
		t.Fatalf("plan.RemoveEvents = %v", plan.RemoveEvents)
	}
	if plan.Empty() {
		t.Fatal("plan should not be empty")
	}
	if !buildAppConfigHealPlan(inSyncHealState()).Empty() {
		t.Fatal("in-sync state should produce an empty plan")
	}
}

func dropString(values []string, drop string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != drop {
			out = append(out, value)
		}
	}
	return out
}
