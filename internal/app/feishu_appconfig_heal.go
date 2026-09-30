package app

import (
	"context"
	"log/slog"
	"sort"
	"strings"
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
func runFeishuAppConfigHeal(a *App) {
	if a == nil || a.feishu == nil {
		return
	}
	cfg := feishuConfig(a)
	if cfg == nil || strings.TrimSpace(cfg.AppID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), feishuAppConfigHealTimeout)
	defer cancel()
	client := newAppConfigHealClient(cfg)
	current, err := client.FetchState(ctx)
	if err != nil {
		slog.Warn("feishu app config heal: fetch state failed",
			"frontend_id", strings.TrimSpace(a.frontendID),
			"app_id", strings.TrimSpace(cfg.AppID),
			"error", err,
		)
		return
	}
	plan := buildAppConfigHealPlan(current)
	if plan.Empty() {
		slog.Info("feishu app config heal: configuration in sync",
			"frontend_id", strings.TrimSpace(a.frontendID),
			"app_id", strings.TrimSpace(cfg.AppID),
			"version", current.OnlineVersion,
		)
		return
	}
	if !current.HasScope(appconfig.PatchScope) {
		slog.Warn("feishu app config heal: patch scope missing; requesting authorization",
			"frontend_id", strings.TrimSpace(a.frontendID),
			"app_id", strings.TrimSpace(cfg.AppID),
		)
		notifyFeishuAppConfigHeal(a, "red", "需要飞书授权",
			feishuAppConfigHealAuthBody(cfg, plan))
		return
	}
	if err := client.ApplyFix(ctx, plan); err != nil {
		slog.Error("feishu app config heal: apply fix failed",
			"frontend_id", strings.TrimSpace(a.frontendID),
			"app_id", strings.TrimSpace(cfg.AppID),
			"error", err,
		)
		notifyFeishuAppConfigHeal(a, "red", "飞书配置自愈失败",
			feishuAppConfigHealFailureBody(plan, err))
		return
	}
	summary := feishuAppConfigHealSummary(plan)
	version, err := client.Publish(ctx, "feidex 自动校准飞书应用配置: "+summary, summary)
	if err != nil {
		slog.Error("feishu app config heal: publish failed",
			"frontend_id", strings.TrimSpace(a.frontendID),
			"app_id", strings.TrimSpace(cfg.AppID),
			"error", err,
		)
		notifyFeishuAppConfigHeal(a, "red", "飞书配置自愈失败",
			feishuAppConfigHealFailureBody(plan, err))
		return
	}
	slog.Info("feishu app config heal: published new version",
		"frontend_id", strings.TrimSpace(a.frontendID),
		"app_id", strings.TrimSpace(cfg.AppID),
		"version", version,
		"changes", summary,
	)
	verified, verifyErr := client.FetchState(ctx)
	if verifyErr == nil && buildAppConfigHealPlan(verified).Empty() {
		_ = a.State().DeleteFrontendCardNotificationsByCollapseKey(feishuAppConfigHealKind)
		notifyFeishuAppConfigHeal(a, "green", "飞书配置已自动修复",
			feishuAppConfigHealSuccessBody(plan, version))
		return
	}
	if verifyErr != nil {
		slog.Warn("feishu app config heal: verify after publish failed",
			"frontend_id", strings.TrimSpace(a.frontendID),
			"app_id", strings.TrimSpace(cfg.AppID),
			"error", verifyErr,
		)
	}
	notifyFeishuAppConfigHeal(a, "orange", "飞书配置修复已提交,等待生效",
		feishuAppConfigHealPendingBody(plan, version))
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

func feishuAppConfigHealPendingBody(plan appconfig.FixPlan, version string) string {
	lines := []string{"修复已提交发布,但线上尚未生效(可能等待审核)。"}
	if value := strings.TrimSpace(version); value != "" {
		lines = append(lines, "已提交版本: "+value)
	}
	if summary := feishuAppConfigHealSummary(plan); summary != "" {
		lines = append(lines, "变更: "+summary)
	}
	lines = append(lines, "如平台提示需要审核,请在开发者后台完成发布审批。")
	return strings.Join(lines, "\n")
}

// notifyFeishuAppConfigHeal delivers one heal status card to every known
// p2p chat of this frontend, falling back to the queued notification path
// (delivered with the next inbound message) when nothing can be sent now.
func notifyFeishuAppConfigHeal(a *App, color, title, body string) {
	if a == nil || strings.TrimSpace(title) == "" || strings.TrimSpace(body) == "" {
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
	for _, target := range feishuAppConfigHealTargets(a) {
		if err := sendFrontendCardNotification(a, target, note); err != nil {
			slog.Warn("feishu app config heal: notify failed",
				"frontend_id", strings.TrimSpace(a.frontendID),
				"chat_id", target.ChatID,
				"error", err,
			)
			continue
		}
		sent = true
	}
	if !sent {
		queueFrontendCardNotification(a, note)
	}
}

func feishuAppConfigHealTargets(a *App) []feishuNotifyTarget {
	if a == nil || a.store == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var targets []feishuNotifyTarget
	for _, sess := range a.State().Sessions() {
		if sess == nil {
			continue
		}
		if !sessionBelongsToFrontend(a, sess.Key) {
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
		targets = append(targets, feishuNotifyTarget{
			ChatID: chatID,
			UserID: strings.TrimSpace(sess.OwnerUserID),
		})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ChatID < targets[j].ChatID })
	return targets
}
