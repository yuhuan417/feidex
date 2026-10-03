// Package upgradecmd provides the daemon upgrade command service extracted
// from the app god package.
package upgradecmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	appdelivery "feidex/internal/adapter/feishu/delivery"
	"feidex/internal/application/upgrade"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Interfaces — what the service needs from the host application
// ---------------------------------------------------------------------------

// UpgradeState narrows app state access to the pending-request operations
// used by the upgrade service.
type UpgradeState interface {
	Pending(id string) *state.PendingRequest
}

type Outbound interface {
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
}

type CardRenderer interface {
	SimpleStatusCard(string, string, string, []feishu.Button) map[string]any
}

// DefaultApp provides an App implementation backed by function callbacks.
type DefaultApp struct {
	ContextFunc              func() context.Context
	OutboundFunc             func() Outbound
	CardRendererFunc         func() CardRenderer
	StateFunc                func() UpgradeState
	CurrentWorkspaceFunc     func(msg *feishu.InboundMessage) (string, *config.Workspace)
	WorkspaceForSessionFunc  func(sessionKey string) *config.Workspace
	RenderPathPickerCardFunc func(requestID string, payload PathPickerPayload) (map[string]any, error)
	DataDirFunc              func() string
	DaemonNameFunc           func() string
	MakeSessionKeyFunc       func(msg *feishu.InboundMessage) string
	ReplyInThreadFunc        func(chatType string) bool
	MenuCardBodyFunc         func(action, body string) string
}

func (a *DefaultApp) UpgradeOutbound() Outbound     { return a.OutboundFunc() }
func (a *DefaultApp) UpgradeRenderer() CardRenderer { return a.CardRendererFunc() }
func (a *DefaultApp) UpgradeState() UpgradeState    { return a.StateFunc() }
func (a *DefaultApp) UpgradeCurrentWorkspace(msg *feishu.InboundMessage) (string, *config.Workspace) {
	return a.CurrentWorkspaceFunc(msg)
}
func (a *DefaultApp) UpgradeWorkspaceForSession(sessionKey string) *config.Workspace {
	return a.WorkspaceForSessionFunc(sessionKey)
}
func (a *DefaultApp) UpgradeRenderPathPickerCard(requestID string, payload PathPickerPayload) (map[string]any, error) {
	return a.RenderPathPickerCardFunc(requestID, payload)
}
func (a *DefaultApp) UpgradeDataDir() string    { return a.DataDirFunc() }
func (a *DefaultApp) DaemonServiceName() string { return a.DaemonNameFunc() }
func (a *DefaultApp) MakeSessionKey(msg *feishu.InboundMessage) string {
	return a.MakeSessionKeyFunc(msg)
}
func (a *DefaultApp) ReplyInThreadEnabled(chatType string) bool { return a.ReplyInThreadFunc(chatType) }
func (a *DefaultApp) MenuCardBody(action, body string) string {
	return a.MenuCardBodyFunc(action, body)
}

// ---------------------------------------------------------------------------
// Exported types
// ---------------------------------------------------------------------------

// UpgradePendingPayload holds the data persisted for a pending upgrade request.
type UpgradePendingPayload = upgrade.Payload

// ---------------------------------------------------------------------------
// Constants and variables
// ---------------------------------------------------------------------------

const (
	// UpgradeLocalBinaryPendingKind is the pending-request kind for local
	// binary upgrades.
	UpgradeLocalBinaryPendingKind = "upgrade_local_binary"
	// UpgradeCommandUsage is the usage string for the /upgrade command.
	UpgradeCommandUsage = "usage: /upgrade | /upgrade dev | /upgrade [VERSION] | /upgrade local | /upgrade path <PATH>"
)

// DisplayLocation is the timezone used when formatting upgrade timestamps.
// Defaults to time.Local; callers may override for tests.
var DisplayLocation = time.Local

// ---------------------------------------------------------------------------
// Service — manages daemon upgrade commands
// ---------------------------------------------------------------------------

// UpgradeServiceDeps holds the function dependencies injected into an
// UpgradeService. These replace the former package-level mutable globals
// and make the service safe for concurrent use.
type UpgradeServiceDeps struct {
	CurrentVersion          func() string
	CurrentGOARCH           func() string
	NormalizeUpgradeVersion func(raw string) (string, error)
	RenderSystemMenuCard    func(sessionKey string) map[string]any
}

// UpgradeService manages daemon upgrade commands for a single app instance.
type UpgradeService struct {
	app     *DefaultApp
	deps    UpgradeServiceDeps
	useCase *upgrade.Service
}

// NewUpgradeService creates a new upgrade service bound to the given app.
func NewUpgradeService(app *DefaultApp, deps UpgradeServiceDeps, useCase *upgrade.Service) UpgradeService {
	return UpgradeService{app: app, deps: deps, useCase: useCase}
}

// ---------------------------------------------------------------------------
// Card rendering methods
// ---------------------------------------------------------------------------

// RenderUpgradePreparingCard renders the "checking for upgrades" card.
func (s UpgradeService) RenderUpgradePreparingCard(sessionKey string) map[string]any {
	body := "正在检查可升级版本，请稍候。\n\n这张卡片会自动刷新。"
	return s.app.UpgradeRenderer().SimpleStatusCard("升级服务", "blue", s.app.MenuCardBody("menu.upgrade", body), nil)
}

// RenderUpgradeFailedCard renders the "upgrade check failed" card.
func (s UpgradeService) RenderUpgradeFailedCard(sessionKey, errText string) map[string]any {
	body := "检查升级信息失败。"
	if text := strings.TrimSpace(errText); text != "" {
		body += "\n\n错误: " + text
	}
	return s.app.UpgradeRenderer().SimpleStatusCard("升级服务", "orange", s.app.MenuCardBody("menu.upgrade", body), UpgradePanelButtons(sessionKey, nil, true))
}

// RenderUpgradeCardForVersion renders the upgrade card for a specific version.
func (s UpgradeService) RenderUpgradeCardForVersion(sessionKey, ownerUserID, requestedVersion string) (map[string]any, error) {
	return s.RenderUpgradeCardForTarget(sessionKey, ownerUserID, requestedVersion, false)
}

// RenderUpgradeDevCard renders the upgrade card for the dev release.
func (s UpgradeService) RenderUpgradeDevCard(sessionKey, ownerUserID string) (map[string]any, error) {
	return s.RenderUpgradeCardForTarget(sessionKey, ownerUserID, "", true)
}

// RenderUpgradeCardForTarget renders the upgrade card for a given target
// (specific version, latest, or dev release).
func (s UpgradeService) RenderUpgradeCardForTarget(sessionKey, ownerUserID, requestedVersion string, useDevRelease bool) (map[string]any, error) {
	result, err := s.useCase.Prepare(s.context(), sessionKey, ownerUserID, requestedVersion, useDevRelease)
	if err != nil {
		return nil, err
	}
	env, target := result.Environment, result.Target
	lines := []string{"当前版本: `" + env.Version + "`", "目标平台: `" + env.GOOS + "/" + env.GOARCH + "`", "目标包: `" + env.AssetName + "`", "二进制: `" + env.Executable + "`"}
	if result.QueryError != nil {
		lines = append(lines, "", "远端版本检查失败。", "错误: "+result.QueryError.Error())
		if env.GOOS == "linux" {
			lines = append(lines, "你仍然可以选择本地 Binary 升级。")
		}
		buttons := upgradeBackButtons(sessionKey)
		if env.GOOS == "linux" {
			buttons = UpgradePanelButtons(sessionKey, nil, true)
		}
		return s.app.UpgradeRenderer().SimpleStatusCard("升级服务", "orange", s.app.MenuCardBody("menu.upgrade", strings.Join(lines, "\n")), buttons), nil
	}
	label := "最新版本"
	if result.Forced {
		label = "指定版本"
	}
	if result.Development {
		label = "开发版本"
	}
	lines = append(lines, label+": `"+target.Version+"`")
	if tag := strings.TrimSpace(target.ReleaseTag); tag != "" && tag != target.Version {
		lines = append(lines, "Release Tag: `"+tag+"`")
	}
	if commit := ShortUpgradeCommit(target.SourceCommit); commit != "" {
		lines = append(lines, "提交: `"+commit+"`")
	}
	if published := FormatUpgradeReleasePublishedAt(target.PublishedAt); published != "" {
		lines = append(lines, "发布时间(本机时区): `"+published+"`")
	}
	if target.HTMLURL != "" {
		lines = append(lines, "Release: <"+target.HTMLURL+">")
	}
	if result.Request != nil {
		lines = append(lines, "", RemoteUpgradeSummary(result.Forced, result.Development))
		return s.RenderUpgradeConfirmCard("升级确认", sessionKey, result.Request.ID, result.Payload, lines), nil
	}
	title, color := "升级服务", "blue"
	if result.Current {
		title, color = "已是最新版本", "green"
		lines = append(lines, "", "当前版本已不落后于远端最新版本。")
	}
	buttons := upgradeBackButtons(sessionKey)
	if env.GOOS == "linux" {
		buttons = UpgradePanelButtons(sessionKey, nil, true)
		lines = append(lines, "你仍然可以选择本地 Binary 升级。")
	} else {
		lines = append(lines, "", "当前平台仅支持 release 检查，不支持自动升级。")
	}
	return s.app.UpgradeRenderer().SimpleStatusCard(title, color, s.app.MenuCardBody("menu.upgrade", strings.Join(lines, "\n")), buttons), nil
}

// ---------------------------------------------------------------------------
// Command handling methods
// ---------------------------------------------------------------------------

// CommandUpgrade handles the /upgrade command with arguments.
func (s UpgradeService) CommandUpgrade(msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		return s.ReplyUpgradeCard(msg, "")
	}
	switch strings.TrimSpace(args[0]) {
	case "dev":
		if len(args) != 1 {
			return errors.New(UpgradeCommandUsage)
		}
		return s.ReplyUpgradeDevCard(msg)
	case "local":
		if len(args) != 1 {
			return errors.New(UpgradeCommandUsage)
		}
		return s.CommandUpgradeLocalPick(msg)
	case "path":
		if len(args) < 2 {
			return errors.New(UpgradeCommandUsage)
		}
		return s.CommandUpgradeLocalPath(msg, strings.Join(args[1:], " "))
	}
	if len(args) > 1 {
		return errors.New(UpgradeCommandUsage)
	}
	targetVersion, err := s.deps.NormalizeUpgradeVersion(args[0])
	if err != nil {
		return fmt.Errorf("版本格式不正确: %q，示例: /upgrade v0.3.0", args[0])
	}
	return s.ReplyUpgradeCard(msg, targetVersion)
}

// ReplyUpgradeCard replies to a message with the upgrade card for the given
// target version.
func (s UpgradeService) ReplyUpgradeCard(msg *feishu.InboundMessage, targetVersion string) error {
	if msg == nil {
		return nil
	}
	card, err := s.RenderUpgradeCardForVersion(s.app.MakeSessionKey(msg), msg.UserID, targetVersion)
	if err != nil {
		return err
	}
	_, err = s.app.UpgradeOutbound().ReplyCard(s.context(), msg.MessageID, card, s.app.ReplyInThreadEnabled(msg.ChatType))
	return err
}

// ReplyUpgradeDevCard replies to a message with the dev upgrade card.
func (s UpgradeService) ReplyUpgradeDevCard(msg *feishu.InboundMessage) error {
	if msg == nil {
		return nil
	}
	card, err := s.RenderUpgradeDevCard(s.app.MakeSessionKey(msg), msg.UserID)
	if err != nil {
		return err
	}
	_, err = s.app.UpgradeOutbound().ReplyCard(s.context(), msg.MessageID, card, s.app.ReplyInThreadEnabled(msg.ChatType))
	return err
}

// ---------------------------------------------------------------------------
// Card action handling
// ---------------------------------------------------------------------------

// CompleteUpgradeAction handles the card action for upgrade confirm/cancel.
func (s UpgradeService) CompleteUpgradeAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}
	id := actionStringValue(action, "request_id")
	if actionName == "upgrade.cancel" {
		req, err := s.useCase.Cancel(id, action.UserID)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已取消升级"}, Card: rawCard(s.deps.RenderSystemMenuCard(req.SessionKey))}, nil
	}
	payload, key, err := s.useCase.Confirm(s.context(), id, action.UserID, action.MessageID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	body := strings.Join([]string{UpgradeStartedSummaryLine(payload), "后台任务: `" + payload.UnitName + "`", "服务即将重启；如果启动失败会自动回退。"}, "\n")
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已开始升级"}, Card: rawCard(s.app.UpgradeRenderer().SimpleStatusCard("升级中", "orange", s.app.MenuCardBody("menu.upgrade", body), upgradeBackButtons(key)))}, nil
}

// ---------------------------------------------------------------------------
// Card building helpers
// ---------------------------------------------------------------------------

// RenderUpgradeConfirmCard builds the upgrade confirmation card.
func (s UpgradeService) RenderUpgradeConfirmCard(title, sessionKey, requestID string, payload UpgradePendingPayload, lines []string) map[string]any {
	buttonLabel := "升级到 " + payload.TargetVersion
	if strings.TrimSpace(payload.SourcePath) != "" {
		buttonLabel = "升级本地制品"
		lines = append(lines,
			"",
			"来源: 本地文件",
			"文件: `"+firstNonEmpty(strings.TrimSpace(payload.SourceName), filepath.Base(payload.SourcePath))+"`",
			"路径: `"+strings.TrimSpace(payload.SourcePath)+"`",
		)
		if payload.SourceSize > 0 {
			lines = append(lines, "大小: `"+appdelivery.FormatDownloadSize(payload.SourceSize)+"`")
		}
		lines = append(lines,
			"sha256: `"+strings.TrimSpace(payload.ExpectedSHA256)+"`",
			"",
			"确认后会使用本地制品重启 daemon；如果启动失败会自动回退到旧版本。",
		)
	}
	return s.app.UpgradeRenderer().SimpleStatusCard(title, "orange", s.app.MenuCardBody("menu.upgrade", strings.Join(lines, "\n")), UpgradePanelButtons(sessionKey, map[string]any{
		"request_id": requestID,
		"label":      buttonLabel,
	}, true))
}

// ---------------------------------------------------------------------------
// Standalone exported helpers
// ---------------------------------------------------------------------------

// RemoteUpgradeSummary returns the summary text for a remote upgrade.
func RemoteUpgradeSummary(forceVersion, useDevRelease bool) string {
	if useDevRelease {
		return "确认后会下载 `dev-latest` 当前指向的开发版构建、重启 daemon；如果启动失败会自动回退到旧版本。"
	}
	if forceVersion {
		return "已跳过最新版本检查。确认后会下载指定版本、重启 daemon；如果启动失败会自动回退到旧版本。"
	}
	return "确认后会下载新版本、重启 daemon；如果启动失败会自动回退到旧版本。"
}

// UpgradePanelButtons builds the button list for the upgrade panel.
func UpgradePanelButtons(sessionKey string, confirm map[string]any, includeBack bool) []feishu.Button {
	buttons := []feishu.Button{}
	if confirm != nil {
		label, _ := confirm["label"].(string)
		if strings.TrimSpace(label) == "" {
			label = "确认升级"
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: "primary",
			Value: map[string]any{
				"action":      "upgrade.confirm",
				"request_id":  confirm["request_id"],
				"session_key": sessionKey,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: "开发版",
		Type: "default",
		Value: map[string]any{
			"action":      "upgrade.dev",
			"session_key": sessionKey,
		},
	})
	buttons = append(buttons, feishu.Button{
		Text: "选择本地 Binary",
		Type: "default",
		Value: map[string]any{
			"action":      "upgrade.local.pick",
			"session_key": sessionKey,
		},
	})
	if includeBack {
		buttons = append(buttons, feishu.Button{
			Text: feishu.MenuBackButtonText,
			Type: "default",
			Value: map[string]any{
				"action":      "menu.group.system",
				"session_key": sessionKey,
			},
		})
	}
	return buttons
}

func upgradeBackButtons(sessionKey string) []feishu.Button {
	return []feishu.Button{
		{
			Text: feishu.MenuBackButtonText,
			Type: "default",
			Value: map[string]any{
				"action":      "menu.group.system",
				"session_key": sessionKey,
			},
		},
	}
}

// UpgradeStartedSummaryLine returns a summary line for the "upgrade started"
// card.
func UpgradeStartedSummaryLine(payload UpgradePendingPayload) string {
	if strings.TrimSpace(payload.SourcePath) != "" {
		return "本地制品: `" + firstNonEmpty(strings.TrimSpace(payload.SourceName), filepath.Base(payload.SourcePath)) + "`"
	}
	line := "目标版本: `" + payload.TargetVersion + "`"
	if tag := strings.TrimSpace(payload.ReleaseTag); tag != "" && tag != strings.TrimSpace(payload.TargetVersion) {
		line += "\nRelease Tag: `" + tag + "`"
	}
	if commit := ShortUpgradeCommit(payload.SourceCommit); commit != "" {
		line += "\n提交: `" + commit + "`"
	}
	return line
}

// ShortUpgradeCommit truncates a commit hash to 12 characters.
func ShortUpgradeCommit(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

// FormatUpgradeReleasePublishedAt formats a release timestamp using the
// configured display location.
func FormatUpgradeReleasePublishedAt(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.In(DisplayLocation).Format("2006-01-02 15:04:05")
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (a *DefaultApp) Context() context.Context {
	if a.ContextFunc != nil {
		return a.ContextFunc()
	}
	return context.Background()
}

func (s UpgradeService) context() context.Context {
	if s.app != nil && s.app.ContextFunc != nil {
		return s.app.ContextFunc()
	}
	return context.Background()
}
