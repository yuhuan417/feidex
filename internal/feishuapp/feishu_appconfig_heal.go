package feishuapp

import (
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	appstate "feidex/internal/adapter/storage/json/scoped"
	frontendapp "feidex/internal/application/frontend"

	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/feishu/appconfig"
	"feidex/internal/state"
)

const (
	// feishuAppConfigHealKind is both the notification kind and the collapse
	// key for heal status cards: newer results replace stale pending ones.
	feishuAppConfigHealKind    = "feishu_app_config_heal"
	feishuAppConfigHealTimeout = 90 * time.Second
)

// Publishing a version does not switch the online version synchronously: the
// platform creates the version, reviews and then promotes it (observed
// publish_time - create_time of several seconds), so the first re-fetch right
// after Publish routinely still sees the old online version. Poll for a
// bounded window before reporting anything, otherwise the same repair reports
// as "已自动修复" or "等待生效" purely depending on timing.
var (
	feishuAppConfigHealPollInterval = 2 * time.Second
	feishuAppConfigHealPollTimeout  = 20 * time.Second
)

// appConfigHealClient is the application-config API surface the heal flow
// needs; it is a seam so tests can substitute a fake.
type appConfigHealClient interface {
	FetchState(ctx context.Context) (*appconfig.State, error)
	ApplyFix(ctx context.Context, plan appconfig.FixPlan) error
	Publish(ctx context.Context, remark, changelog string) (string, error)
}

var newAppConfigHealClient = func(cfg *config.FeishuConfig) appConfigHealClient {
	return appconfig.NewClient(cfg)
}

type FeishuAppConfigHealInputs struct {
	Client        FeishuClient
	Config        *config.Config
	ConfigMu      *sync.RWMutex
	ConfigIndex   int
	FrontendID    string
	State         *appstate.Store
	Context       func() context.Context
	Notifications frontendapp.Notifications
}

func (i FeishuAppConfigHealInputs) feishuConfig() *config.FeishuConfig {
	return (frontendConfigView{cfg: i.Config, mu: i.ConfigMu, frontendConfigIndex: i.ConfigIndex}).feishuConfig()
}

// runFeishuAppConfigHeal aligns the platform-side application configuration
// (scopes and event subscriptions) with the requirements compiled into this
// binary. It runs once per frontend at startup, asynchronously:
//
//   - no drift: log only;
//   - drift and no application:application:patch grant: send a card with the
//     console auth link so the owner can grant the missing scopes;
//   - drift and patch granted: patch the configuration, publish a new
//     version, verify, and report the outcome with a card.
//
// Every failure path only logs or notifies; it never blocks startup.
func runFeishuAppConfigHealWith(inputs FeishuAppConfigHealInputs) {
	if inputs.Client == nil {
		return
	}
	cfg := inputs.feishuConfig()
	if cfg == nil || strings.TrimSpace(cfg.AppID) == "" {
		return
	}
	baseContext := context.Background()
	if inputs.Context != nil && inputs.Context() != nil {
		baseContext = inputs.Context()
	}
	ctx, cancel := context.WithTimeout(baseContext, feishuAppConfigHealTimeout)
	defer cancel()
	client := newAppConfigHealClient(cfg)
	current, err := client.FetchState(ctx)
	if err != nil {
		slog.Warn("feishu app config heal: fetch state failed",
			"frontend_id", strings.TrimSpace(inputs.FrontendID),
			"app_id", strings.TrimSpace(cfg.AppID),
			"error", err,
		)
		return
	}
	plan := buildAppConfigHealPlan(current)
	if plan.Empty() {
		slog.Info("feishu app config heal: configuration in sync",
			"frontend_id", strings.TrimSpace(inputs.FrontendID),
			"app_id", strings.TrimSpace(cfg.AppID),
			"version", current.OnlineVersion,
		)
		return
	}
	if unauditVersionID := strings.TrimSpace(current.UnauditVersionID); unauditVersionID != "" {
		// A version is already waiting for review. Submitting another one would
		// pile up versions and still not take effect, so report and wait: the
		// drift we see is measured against the online version, which only
		// changes once that review passes.
		slog.Info("feishu app config heal: version already under audit; waiting",
			"frontend_id", strings.TrimSpace(inputs.FrontendID),
			"app_id", strings.TrimSpace(cfg.AppID),
			"unaudit_version_id", unauditVersionID,
		)
		notifyFeishuAppConfigHeal(inputs, "orange", "飞书配置修复等待审核",
			feishuAppConfigHealUnderAuditBody(plan, current.OnlineVersion, unauditVersionID))
		return
	}
	if !current.HasScope(appconfig.PatchScope) {
		slog.Warn("feishu app config heal: patch scope missing; requesting authorization",
			"frontend_id", strings.TrimSpace(inputs.FrontendID),
			"app_id", strings.TrimSpace(cfg.AppID),
		)
		notifyFeishuAppConfigHeal(inputs, "red", "需要飞书授权",
			feishuAppConfigHealAuthBody(cfg, plan))
		return
	}
	if err := client.ApplyFix(ctx, plan); err != nil {
		slog.Error("feishu app config heal: apply fix failed",
			"frontend_id", strings.TrimSpace(inputs.FrontendID),
			"app_id", strings.TrimSpace(cfg.AppID),
			"error", err,
		)
		notifyFeishuAppConfigHeal(inputs, "red", "飞书配置自愈失败",
			feishuAppConfigHealFailureBody(plan, err))
		return
	}
	summary := feishuAppConfigHealSummary(plan)
	version, err := client.Publish(ctx, "feidex 自动校准飞书应用配置: "+summary, summary)
	if err != nil {
		slog.Error("feishu app config heal: publish failed",
			"frontend_id", strings.TrimSpace(inputs.FrontendID),
			"app_id", strings.TrimSpace(cfg.AppID),
			"error", err,
		)
		notifyFeishuAppConfigHeal(inputs, "red", "飞书配置自愈失败",
			feishuAppConfigHealFailureBody(plan, err))
		return
	}
	slog.Info("feishu app config heal: published new version",
		"frontend_id", strings.TrimSpace(inputs.FrontendID),
		"app_id", strings.TrimSpace(cfg.AppID),
		"version", version,
		"changes", summary,
	)
	outcome := waitForAppConfigHeal(ctx, client, plan, version)
	slog.Info("feishu app config heal: verified published version",
		"frontend_id", strings.TrimSpace(inputs.FrontendID),
		"app_id", strings.TrimSpace(cfg.AppID),
		"published_version", version,
		"outcome", outcome.kind.String(),
		"online_version", outcome.onlineVersion,
		"online_version_status", outcome.onlineVersionStatus,
		"unaudit_version_id", outcome.unauditVersionID,
		"remaining_changes", feishuAppConfigHealSummary(outcome.remaining),
	)
	switch outcome.kind {
	case appConfigHealVerified:
		_ = inputs.Notifications.Clear(feishuAppConfigHealKind)
		notifyFeishuAppConfigHeal(inputs, "green", "飞书配置已自动修复",
			feishuAppConfigHealSuccessBody(plan, version))
	case appConfigHealUnderAudit:
		notifyFeishuAppConfigHeal(inputs, "orange", "飞书配置修复等待审核",
			feishuAppConfigHealUnderAuditBody(plan, version, outcome.unauditVersionID))
	case appConfigHealNotPromoted:
		notifyFeishuAppConfigHeal(inputs, "orange", "飞书配置修复已提交,等待生效",
			feishuAppConfigHealPendingBody(plan, version, outcome.onlineVersion))
	case appConfigHealVerifyFailed:
		notifyFeishuAppConfigHeal(inputs, "orange", "飞书配置修复已提交,复查失败",
			feishuAppConfigHealVerifyFailedBody(plan, version, outcome.err))
	default:
		notifyFeishuAppConfigHeal(inputs, "red", "飞书配置修复未完全生效",
			feishuAppConfigHealMismatchBody(plan, version, outcome))
	}
}

// appConfigHealOutcomeKind classifies what the post-publish verification saw.
type appConfigHealOutcomeKind int

const (
	// appConfigHealVerified: the published version is online and the plan is empty.
	appConfigHealVerified appConfigHealOutcomeKind = iota
	// appConfigHealUnderAudit: a version is still being reviewed.
	appConfigHealUnderAudit
	// appConfigHealNotPromoted: no review pending, but the online version is
	// still the previous one.
	appConfigHealNotPromoted
	// appConfigHealVerifyFailed: every re-fetch failed.
	appConfigHealVerifyFailed
	// appConfigHealMismatch: our version is online yet the plan is not empty.
	appConfigHealMismatch
)

func (k appConfigHealOutcomeKind) String() string {
	switch k {
	case appConfigHealVerified:
		return "verified"
	case appConfigHealUnderAudit:
		return "under_audit"
	case appConfigHealNotPromoted:
		return "not_promoted"
	case appConfigHealVerifyFailed:
		return "verify_failed"
	default:
		return "mismatch"
	}
}

type appConfigHealOutcome struct {
	kind                appConfigHealOutcomeKind
	onlineVersion       string
	onlineVersionStatus int
	unauditVersionID    string
	remaining           appconfig.FixPlan
	err                 error
}

// waitForAppConfigHeal re-reads the platform state until the published version
// is online with all required scopes and events, or the poll window expires.
// The first attempt runs immediately; later attempts wait for the interval.
func waitForAppConfigHeal(ctx context.Context, client appConfigHealClient, plan appconfig.FixPlan, publishedVersion string) appConfigHealOutcome {
	deadline := time.Now().Add(feishuAppConfigHealPollTimeout)
	outcome := appConfigHealOutcome{kind: appConfigHealVerifyFailed}
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if time.Now().After(deadline) {
				break
			}
			select {
			case <-ctx.Done():
				outcome.err = ctx.Err()
				return outcome
			case <-time.After(feishuAppConfigHealPollInterval):
			}
		}
		state, err := client.FetchState(ctx)
		if err != nil {
			outcome.err = err
			continue
		}
		outcome.err = nil
		outcome.onlineVersion = strings.TrimSpace(state.OnlineVersion)
		outcome.onlineVersionStatus = state.OnlineVersionStatus
		outcome.unauditVersionID = strings.TrimSpace(state.UnauditVersionID)
		outcome.remaining = buildAppConfigHealPlan(state)
		if outcome.remaining.Empty() {
			outcome.kind = appConfigHealVerified
			return outcome
		}
	}
	switch {
	case outcome.onlineVersion == "" && outcome.err != nil:
		outcome.kind = appConfigHealVerifyFailed
	case outcome.unauditVersionID != "":
		outcome.kind = appConfigHealUnderAudit
	case strings.TrimSpace(outcome.onlineVersion) == strings.TrimSpace(publishedVersion) && outcome.onlineVersion != "":
		// Our version is online but the configuration still disagrees.
		outcome.kind = appConfigHealMismatch
	default:
		outcome.kind = appConfigHealNotPromoted
	}
	return outcome
}

// buildAppConfigHealPlan compares the binary's requirements against the
// published application configuration.
func buildAppConfigHealPlan(current *appconfig.State) appconfig.FixPlan {
	var plan appconfig.FixPlan
	if current == nil {
		return plan
	}
	requiredScopes := appconfig.RequiredScopes()
	requiredEvents := feishu.RequiredEventTypes()
	plan.AddScopes = sortedStringDiff(requiredScopes, current.VersionScopes)
	plan.AddEvents = sortedStringDiff(requiredEvents, current.VersionEvents)
	plan.RemoveEvents = sortedStringDiff(current.VersionEvents, requiredEvents)
	return plan
}

// sortedStringDiff returns the sorted entries of want that are not in have.
func sortedStringDiff(want, have []string) []string {
	if len(want) == 0 {
		return nil
	}
	haveSet := make(map[string]struct{}, len(have))
	for _, value := range have {
		haveSet[value] = struct{}{}
	}
	var out []string
	for _, value := range want {
		if value == "" {
			continue
		}
		if _, ok := haveSet[value]; ok {
			continue
		}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func feishuAppConfigHealSummary(plan appconfig.FixPlan) string {
	parts := make([]string, 0, 3)
	if len(plan.AddScopes) > 0 {
		parts = append(parts, "新增权限 "+strings.Join(plan.AddScopes, "、"))
	}
	if len(plan.AddEvents) > 0 {
		parts = append(parts, "新增事件订阅 "+strings.Join(plan.AddEvents, "、"))
	}
	if len(plan.RemoveEvents) > 0 {
		parts = append(parts, "移除事件订阅 "+strings.Join(plan.RemoveEvents, "、"))
	}
	return strings.Join(parts, "; ")
}

func feishuAppConfigHealAuthBody(cfg *config.FeishuConfig, plan appconfig.FixPlan) string {
	scopes := uniqueStrings(append([]string{appconfig.PatchScope}, plan.AddScopes...))
	url := appconfig.AuthURL(cfg.OpenBaseURL(), cfg.AppID, scopes)
	lines := []string{
		"检测到机器人配置与代码需求不一致,但缺少自动修复所需的权限。",
		"",
		"需要授权的权限: " + strings.Join(scopes, "、"),
	}
	if summary := feishuAppConfigHealSummary(plan); summary != "" {
		lines = append(lines, "待调整配置: "+summary)
	}
	lines = append(lines, "",
		"申请链接: "+feishu.MarkdownLink("为 "+strings.TrimSpace(cfg.AppID)+" 申请权限", url),
		"授权后,机器人下次启动会自动完成剩余配置并发布。",
	)
	return strings.Join(lines, "\n")
}

func feishuAppConfigHealFailureBody(plan appconfig.FixPlan, err error) string {
	lines := []string{"自动校准飞书应用配置失败。"}
	if summary := feishuAppConfigHealSummary(plan); summary != "" {
		lines = append(lines, "待调整配置: "+summary)
	}
	if err != nil {
		lines = append(lines, "错误: `"+feishu.EscapeInlineBackticks(err.Error())+"`")
	}
	lines = append(lines, "机器人会在下次启动时重试。")
	return strings.Join(lines, "\n")
}

func feishuAppConfigHealSuccessBody(plan appconfig.FixPlan, version string) string {
	lines := []string{"已自动完成飞书应用配置校准,线上版本已生效。"}
	if value := strings.TrimSpace(version); value != "" {
		lines = append(lines, "新版本: "+value)
	}
	if summary := feishuAppConfigHealSummary(plan); summary != "" {
		lines = append(lines, "变更: "+summary)
	}
	return strings.Join(lines, "\n")
}

func feishuAppConfigHealPendingBody(plan appconfig.FixPlan, version, onlineVersion string) string {
	lines := []string{"修复已提交发布,但线上版本尚未切换。"}
	if value := strings.TrimSpace(version); value != "" {
		lines = append(lines, "已提交版本: "+value)
	}
	if value := strings.TrimSpace(onlineVersion); value != "" {
		lines = append(lines, "当前线上版本: "+value)
	}
	if summary := feishuAppConfigHealSummary(plan); summary != "" {
		lines = append(lines, "变更: "+summary)
	}
	lines = append(lines, "没有版本处于审核中;平台通常在数秒内完成切换,机器人下次启动会再次确认。")
	return strings.Join(lines, "\n")
}

func feishuAppConfigHealUnderAuditBody(plan appconfig.FixPlan, version, unauditVersionID string) string {
	lines := []string{"修复已提交发布,当前有版本处于审核中,审核通过后会自动生效。"}
	if value := strings.TrimSpace(version); value != "" {
		lines = append(lines, "已提交版本: "+value)
	}
	if value := strings.TrimSpace(unauditVersionID); value != "" {
		lines = append(lines, "审核中版本 ID: `"+feishu.EscapeInlineBackticks(value)+"`")
	}
	if summary := feishuAppConfigHealSummary(plan); summary != "" {
		lines = append(lines, "变更: "+summary)
	}
	lines = append(lines, "请在开发者后台完成发布审批;不需要重新触发修复。")
	return strings.Join(lines, "\n")
}

func feishuAppConfigHealVerifyFailedBody(plan appconfig.FixPlan, version string, err error) string {
	lines := []string{"修复已提交发布,但复查线上配置失败,无法确认是否生效。"}
	if value := strings.TrimSpace(version); value != "" {
		lines = append(lines, "已提交版本: "+value)
	}
	if summary := feishuAppConfigHealSummary(plan); summary != "" {
		lines = append(lines, "变更: "+summary)
	}
	if err != nil {
		lines = append(lines, "错误: `"+feishu.EscapeInlineBackticks(err.Error())+"`")
	}
	lines = append(lines, "机器人下次启动会再次复查。")
	return strings.Join(lines, "\n")
}

func feishuAppConfigHealMismatchBody(plan appconfig.FixPlan, version string, outcome appConfigHealOutcome) string {
	lines := []string{"已提交的版本已在线,但配置仍未满足要求。"}
	if value := strings.TrimSpace(version); value != "" {
		lines = append(lines, "已提交版本: "+value)
	}
	if value := strings.TrimSpace(outcome.onlineVersion); value != "" {
		lines = append(lines, "当前线上版本: "+value)
	}
	if summary := feishuAppConfigHealSummary(outcome.remaining); summary != "" {
		lines = append(lines, "仍缺少: "+summary)
	}
	lines = append(lines, "可能是平台未接受部分变更,请在开发者后台核对后手动处理。")
	return strings.Join(lines, "\n")
}

// notifyFeishuAppConfigHeal delivers one heal status card to every known
// p2p chat of this frontend, falling back to the queued notification path
// (delivered with the next inbound message) when nothing can be sent now.
func notifyFeishuAppConfigHeal(inputs FeishuAppConfigHealInputs, color, title, body string) {
	if strings.TrimSpace(title) == "" || strings.TrimSpace(body) == "" {
		return
	}
	note := state.FrontendCardNotification{
		Kind:        feishuAppConfigHealKind,
		CollapseKey: feishuAppConfigHealKind,
		Title:       title,
		Color:       color,
		Body:        body,
	}
	sent := false
	ctx := context.Background()
	if inputs.Context != nil && inputs.Context() != nil {
		ctx = inputs.Context()
	}
	for _, target := range feishuAppConfigHealTargets(inputs.State, inputs.FrontendID) {
		if err := inputs.Notifications.Sender.DeliverNotification(ctx, target.ChatID, target.UserID, note); err != nil {
			slog.Warn("feishu app config heal: notify failed",
				"frontend_id", strings.TrimSpace(inputs.FrontendID),
				"chat_id", target.ChatID,
				"error", err,
			)
			continue
		}
		sent = true
	}
	if !sent {
		inputs.Notifications.Queue(note)
	}
}

func feishuAppConfigHealTargets(store *appstate.Store, frontendID string) []appfeishuwrap.NotifyTarget {
	if store == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var targets []appfeishuwrap.NotifyTarget
	for _, sess := range store.Sessions() {
		if sess == nil {
			continue
		}
		if !sessionBelongsToFrontend(frontendID, sess.Key) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(sess.ChatType), "p2p") {
			continue
		}
		chatID := strings.TrimSpace(sess.ChatID)
		if chatID == "" {
			continue
		}
		if _, ok := seen[chatID]; ok {
			continue
		}
		seen[chatID] = struct{}{}
		targets = append(targets, appfeishuwrap.NotifyTarget{
			ChatID: chatID,
			UserID: strings.TrimSpace(sess.OwnerUserID),
		})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ChatID < targets[j].ChatID })
	return targets
}
