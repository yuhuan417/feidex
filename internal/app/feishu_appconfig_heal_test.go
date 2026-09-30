package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/feishu/appconfig"
)

type fakeAppConfigHealClient struct {
	state      *appconfig.State
	stateAfter *appconfig.State
	// states, when set, is an explicit per-call sequence; the last entry
	// repeats. It models the platform promoting a version after a delay.
	states []*appconfig.State
	// fetchErr, when set, is returned for every fetch after the first one.
	fetchErr   error
	fetchCalls int
	fixes      []appconfig.FixPlan
	publishes  []string
	applyErr   error
	publishErr error
}

func (f *fakeAppConfigHealClient) FetchState(context.Context) (*appconfig.State, error) {
	index := f.fetchCalls
	f.fetchCalls++
	if f.fetchErr != nil && index > 0 {
		return nil, f.fetchErr
	}
	if len(f.states) > 0 {
		if index >= len(f.states) {
			index = len(f.states) - 1
		}
		return f.states[index], nil
	}
	if index > 0 && f.stateAfter != nil {
		return f.stateAfter, nil
	}
	return f.state, nil
}

// withFastHealPolling shrinks the post-publish poll window so tests do not
// wait for the real one.
func withFastHealPolling(t *testing.T) {
	t.Helper()
	prevInterval, prevTimeout := feishuAppConfigHealPollInterval, feishuAppConfigHealPollTimeout
	feishuAppConfigHealPollInterval = time.Millisecond
	feishuAppConfigHealPollTimeout = 20 * time.Millisecond
	t.Cleanup(func() {
		feishuAppConfigHealPollInterval, feishuAppConfigHealPollTimeout = prevInterval, prevTimeout
	})
}

// driftedHealState returns an in-sync state with one scope missing.
func driftedHealState() *appconfig.State {
	drifted := inSyncHealState()
	drifted.VersionScopes = dropString(drifted.VersionScopes, "drive:drive")
	return drifted
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
	drifted := driftedHealState()
	drifted.VersionEvents = append(drifted.VersionEvents, "im.message.message_read_v1")
	fake := &fakeAppConfigHealClient{state: drifted, stateAfter: inSyncHealState()}
	withFakeHealClient(t, fake)
	withFastHealPolling(t)

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
	drifted := driftedHealState()
	fake := &fakeAppConfigHealClient{state: drifted, stateAfter: drifted}
	withFakeHealClient(t, fake)
	withFastHealPolling(t)

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

// The platform promotes a published version asynchronously, so the first
// re-fetch may still see the old online version. The heal must keep polling
// and report success once it flips, instead of reporting "等待生效" purely
// because it sampled too early.
func TestFeishuAppConfigHealWaitsForVersionPromotion(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.AppID = "cli_test"
	withFastHealPolling(t)
	promoted := inSyncHealState()
	promoted.OnlineVersion = "1.0.10"
	fake := &fakeAppConfigHealClient{states: []*appconfig.State{driftedHealState(), driftedHealState(), promoted}}
	withFakeHealClient(t, fake)

	runFeishuAppConfigHeal(a)

	notes := a.State().FrontendCardNotifications()
	if len(notes) != 1 || notes[0].Title != "飞书配置已自动修复" {
		t.Fatalf("card = %+v, want the success card after the version was promoted", notes)
	}
	if fake.fetchCalls != 3 {
		t.Fatalf("fetch calls = %d, want the heal to poll until the version was online", fake.fetchCalls)
	}
}

// A version parked in review must be reported as such, not as a generic wait.
func TestFeishuAppConfigHealReportsAuditPending(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.AppID = "cli_test"
	withFastHealPolling(t)
	underAudit := driftedHealState()
	underAudit.UnauditVersionID = "oav_under_audit"
	fake := &fakeAppConfigHealClient{states: []*appconfig.State{driftedHealState(), underAudit}}
	withFakeHealClient(t, fake)

	runFeishuAppConfigHeal(a)

	notes := a.State().FrontendCardNotifications()
	if len(notes) != 1 || notes[0].Title != "飞书配置修复等待审核" {
		t.Fatalf("card = %+v, want the audit-pending card", notes)
	}
	if !strings.Contains(notes[0].Body, "oav_under_audit") {
		t.Fatalf("card body = %q, want the version under audit", notes[0].Body)
	}
}

// A verification that cannot reach the platform must not be reported as an
// audit wait.
func TestFeishuAppConfigHealReportsVerifyFailure(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.AppID = "cli_test"
	withFastHealPolling(t)
	fake := &fakeAppConfigHealClient{state: driftedHealState(), fetchErr: errors.New("connection reset")}
	withFakeHealClient(t, fake)

	runFeishuAppConfigHeal(a)

	notes := a.State().FrontendCardNotifications()
	if len(notes) != 1 || notes[0].Title != "飞书配置修复已提交,复查失败" {
		t.Fatalf("card = %+v, want the verify-failure card", notes)
	}
	if !strings.Contains(notes[0].Body, "connection reset") {
		t.Fatalf("card body = %q, want the fetch error", notes[0].Body)
	}
}

// If our own version is online and the configuration still disagrees, the
// repair did not land and the owner has to look at the console.
func TestFeishuAppConfigHealReportsMismatchWhenOwnVersionIsOnline(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.AppID = "cli_test"
	withFastHealPolling(t)
	stillDrifted := driftedHealState()
	stillDrifted.OnlineVersion = "1.0.10" // the version Publish reported
	fake := &fakeAppConfigHealClient{states: []*appconfig.State{driftedHealState(), stillDrifted}}
	withFakeHealClient(t, fake)

	runFeishuAppConfigHeal(a)

	notes := a.State().FrontendCardNotifications()
	if len(notes) != 1 || notes[0].Title != "飞书配置修复未完全生效" {
		t.Fatalf("card = %+v, want the mismatch card", notes)
	}
	if !strings.Contains(notes[0].Body, "drive:drive") {
		t.Fatalf("card body = %q, want the remaining drift", notes[0].Body)
	}
}

// A version already waiting for review must stop the heal from piling up more
// versions on every startup: report and wait instead of patching again.
func TestFeishuAppConfigHealWaitsWhenVersionAlreadyUnderAudit(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.AppID = "cli_test"
	drifted := driftedHealState()
	drifted.UnauditVersionID = "oav_pending_from_last_startup"
	fake := &fakeAppConfigHealClient{state: drifted, stateAfter: inSyncHealState()}
	withFakeHealClient(t, fake)

	runFeishuAppConfigHeal(a)

	if len(fake.fixes) != 0 || len(fake.publishes) != 0 {
		t.Fatalf("fixes = %v, publishes = %v; want no new submission while a version is under audit", fake.fixes, fake.publishes)
	}
	if fake.fetchCalls != 1 {
		t.Fatalf("fetch calls = %d, want the heal to stop after the first read", fake.fetchCalls)
	}
	notes := a.State().FrontendCardNotifications()
	if len(notes) != 1 || notes[0].Title != "飞书配置修复等待审核" {
		t.Fatalf("card = %+v, want the audit-pending card", notes)
	}
	if !strings.Contains(notes[0].Body, "oav_pending_from_last_startup") {
		t.Fatalf("card body = %q, want the version under audit", notes[0].Body)
	}
}
