// Package upgradecmd provides the daemon upgrade command service extracted
// from the app god package.
package upgradecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	appdelivery "feidex/internal/app/delivery"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/release"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Interfaces — what the service needs from the host application
// ---------------------------------------------------------------------------

// ReleaseClient abstracts the release query client.
type ReleaseClient interface {
	LatestLinuxBinary(ctx context.Context, goarch string) (*release.ReleaseInfo, error)
	LatestDevLinuxBinary(ctx context.Context, goarch string) (*release.ReleaseInfo, error)
	LinuxBinaryByVersion(ctx context.Context, version, goarch string) (*release.ReleaseInfo, error)
}

// UpgradeState narrows app state access to the pending-request operations
// used by the upgrade service.
type UpgradeState interface {
	NextLocalID(prefix string) (string, error)
	SavePending(req *state.PendingRequest) error
	Pending(id string) *state.PendingRequest
	UpdatePending(id string, mutate func(*state.PendingRequest)) error
}

// App defines the interface the upgrade service requires from the host
// application.
type App interface {
	// UpgradeFeishu returns the Feishu bot client.
	UpgradeFeishu() FeishuClient
	// UpgradeState returns the narrowed app state provider for upgrade ops.
	UpgradeState() UpgradeState
	// UpgradeCurrentWorkspace resolves the active session key and workspace
	// for an inbound message.
	UpgradeCurrentWorkspace(msg *feishu.InboundMessage) (string, *config.Workspace)
	// UpgradeWorkspaceForSession resolves the current workspace for a session.
	UpgradeWorkspaceForSession(sessionKey string) *config.Workspace
	// UpgradeRenderPathPickerCard renders the path picker card used by the
	// local-upgrade flow.
	UpgradeRenderPathPickerCard(requestID string, payload PathPickerPayload) (map[string]any, error)
	// UpgradeDataDir returns the application data directory for staged local
	// upgrade artifacts.
	UpgradeDataDir() string
	// DaemonServiceName returns the daemon service name from config (thread-safe).
	DaemonServiceName() string
	// MakeSessionKey builds a session key from an inbound message.
	MakeSessionKey(msg *feishu.InboundMessage) string
	// ReplyInThreadEnabled reports whether reply-in-thread is enabled for the
	// given chat type.
	ReplyInThreadEnabled(chatType string) bool
	// MenuCardBody formats a menu card body with breadcrumb navigation.
	MenuCardBody(action, body string) string
}

// FeishuClient is the narrow interface for the Feishu bot client methods
// used by the upgrade service.
type FeishuClient interface {
	SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any
	ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error)
}

// DefaultApp provides an App implementation backed by function callbacks.
type DefaultApp struct {
	FeishuClientFunc         func() FeishuClient
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

func (a *DefaultApp) UpgradeFeishu() FeishuClient { return a.FeishuClientFunc() }
func (a *DefaultApp) UpgradeState() UpgradeState  { return a.StateFunc() }
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
type UpgradePendingPayload struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	ReleaseTag     string `json:"release_tag"`
	BinaryPath     string `json:"binary_path"`
	DownloadURL    string `json:"download_url"`
	SourcePath     string `json:"source_path"`
	SourceKind     string `json:"source_kind"`
	SourceName     string `json:"source_name"`
	SourceSize     int64  `json:"source_size"`
	SourceCommit   string `json:"source_commit"`
	ExpectedSHA256 string `json:"expected_sha256"`
	ReleaseURL     string `json:"release_url"`
	UnitName       string `json:"unit_name,omitempty"`
	ChatID         string `json:"chat_id,omitempty"`
	FeishuMsgID    string `json:"feishu_msg_id,omitempty"`
}

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

// Function variables — injected by the parent package, overridable in tests.
var (
	// CurrentVersion returns the current binary version.
	CurrentVersion func() string
	// CurrentGOOS returns the target operating system.
	CurrentGOOS func() string
	// CurrentGOARCH returns the target architecture.
	CurrentGOARCH func() string
	// NewReleaseClient creates a new release query client.
	NewReleaseClient func() ReleaseClient
	// NewDaemonManager creates a new daemon manager for the given service name.
	NewDaemonManager func(serviceName string) (daemon.Manager, error)
	// StartDaemonUpgrade starts a background daemon upgrade.
	StartDaemonUpgrade func(spec daemon.UpgradeSpec) (string, error)
	// NormalizeUpgradeVersion normalizes a user-supplied version string.
	NormalizeUpgradeVersion func(raw string) (string, error)
	// RenderSystemMenuCard renders the system menu card for the given session.
	RenderSystemMenuCard func(sessionKey string) map[string]any
)

// ---------------------------------------------------------------------------
// Service — manages daemon upgrade commands
// ---------------------------------------------------------------------------

// UpgradeService manages daemon upgrade commands for a single app instance.
type UpgradeService struct {
	app App
}

// NewUpgradeService creates a new upgrade service bound to the given app.
func NewUpgradeService(app App) UpgradeService {
	return UpgradeService{app: app}
}

// ---------------------------------------------------------------------------
// Card rendering methods
// ---------------------------------------------------------------------------

// RenderUpgradePreparingCard renders the "checking for upgrades" card.
func (s UpgradeService) RenderUpgradePreparingCard(sessionKey string) map[string]any {
	body := "正在检查可升级版本，请稍候。\n\n这张卡片会自动刷新。"
	return s.app.UpgradeFeishu().SimpleStatusCard("升级服务", "blue", s.app.MenuCardBody("menu.upgrade", body), nil)
}

// RenderUpgradeFailedCard renders the "upgrade check failed" card.
func (s UpgradeService) RenderUpgradeFailedCard(sessionKey, errText string) map[string]any {
	body := "检查升级信息失败。"
	if text := strings.TrimSpace(errText); text != "" {
		body += "\n\n错误: " + text
	}
	return s.app.UpgradeFeishu().SimpleStatusCard("升级服务", "orange", s.app.MenuCardBody("menu.upgrade", body), UpgradePanelButtons(sessionKey, nil, true))
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
	st := s.app.UpgradeState()
	goos := strings.TrimSpace(CurrentGOOS())
	var exePath, assetName string
	var err error
	if goos == "linux" {
		exePath, assetName, err = s.ValidateUpgradeRuntime()
	} else {
		exePath, assetName, err = s.probeUpgradeRuntime()
	}
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	current := CurrentVersion()
	goarch := strings.TrimSpace(CurrentGOARCH())
	bodyLines := []string{
		"当前版本: `" + current + "`",
		"目标平台: `" + firstNonEmpty(goos, "unknown") + "/" + firstNonEmpty(goarch, "unknown") + "`",
		"目标包: `" + assetName + "`",
		"二进制: `" + exePath + "`",
	}

	target := &release.ReleaseInfo{}
	forceVersion := strings.TrimSpace(requestedVersion) != ""
	switch {
	case useDevRelease:
		target, err = NewReleaseClient().LatestDevLinuxBinary(ctx, goarch)
		if err != nil {
			if goos != "linux" {
				bodyLines = append(bodyLines, "", "远端版本检查失败。当前平台仅支持 release 检查。", "错误: "+err.Error())
				return s.app.UpgradeFeishu().SimpleStatusCard("升级服务", "orange", s.app.MenuCardBody("menu.upgrade", strings.Join(bodyLines, "\n")), upgradeBackButtons(sessionKey)), nil
			}
			return nil, fmt.Errorf("查询开发版 %s 失败: %w", release.DevReleaseTag, err)
		}
		bodyLines = append(bodyLines, "开发版本: `"+target.Version+"`")
		if strings.TrimSpace(target.ReleaseTag) != "" && strings.TrimSpace(target.ReleaseTag) != strings.TrimSpace(target.Version) {
			bodyLines = append(bodyLines, "Release Tag: `"+target.ReleaseTag+"`")
		}
		if commit := ShortUpgradeCommit(target.SourceCommit); commit != "" {
			bodyLines = append(bodyLines, "提交: `"+commit+"`")
		}
	case forceVersion:
		target, err = NewReleaseClient().LinuxBinaryByVersion(ctx, requestedVersion, goarch)
		if err != nil {
			if goos != "linux" {
				bodyLines = append(bodyLines, "", "远端版本检查失败。当前平台仅支持 release 检查。", "错误: "+err.Error())
				return s.app.UpgradeFeishu().SimpleStatusCard("升级服务", "orange", s.app.MenuCardBody("menu.upgrade", strings.Join(bodyLines, "\n")), upgradeBackButtons(sessionKey)), nil
			}
			return nil, fmt.Errorf("查询指定版本 %s 失败: %w", requestedVersion, err)
		}
		bodyLines = append(bodyLines, "指定版本: `"+target.Version+"`")
	default:
		target, err = NewReleaseClient().LatestLinuxBinary(ctx, goarch)
		if err != nil {
			if goos != "linux" {
				bodyLines = append(bodyLines, "", "远端版本检查失败。当前平台仅支持 release 检查。", "错误: "+err.Error())
				return s.app.UpgradeFeishu().SimpleStatusCard("升级服务", "orange", s.app.MenuCardBody("menu.upgrade", strings.Join(bodyLines, "\n")), upgradeBackButtons(sessionKey)), nil
			}
			bodyLines = append(bodyLines, "", "远端版本检查失败。你仍然可以选择本地 Binary 升级。", "错误: "+err.Error())
			return s.app.UpgradeFeishu().SimpleStatusCard("升级服务", "orange", s.app.MenuCardBody("menu.upgrade", strings.Join(bodyLines, "\n")), UpgradePanelButtons(sessionKey, nil, true)), nil
		}
		bodyLines = append(bodyLines, "最新版本: `"+target.Version+"`")
	}
	if published := FormatUpgradeReleasePublishedAt(target.PublishedAt); published != "" {
		bodyLines = append(bodyLines, "发布时间(本机时区): `"+published+"`")
	}
	if strings.TrimSpace(target.HTMLURL) != "" {
		bodyLines = append(bodyLines, "Release: <"+target.HTMLURL+">")
	}

	if goos != "linux" {
		bodyLines = append(bodyLines, "", "当前平台仅支持 release 检查，不支持自动升级。")
		title := "升级服务"
		color := "blue"
		if !forceVersion && !useDevRelease {
			if cmp, cmpErr := release.CompareVersions(current, target.Version); cmpErr == nil && cmp >= 0 {
				title = "已是最新版本"
				color = "green"
				bodyLines = append(bodyLines, "", "当前版本已不落后于远端最新版本。")
			}
		}
		return s.app.UpgradeFeishu().SimpleStatusCard(title, color, s.app.MenuCardBody("menu.upgrade", strings.Join(bodyLines, "\n")), upgradeBackButtons(sessionKey)), nil
	}

	if !forceVersion && !useDevRelease {
		if cmp, cmpErr := release.CompareVersions(current, target.Version); cmpErr == nil && cmp >= 0 {
			bodyLines = append(bodyLines, "", "当前版本已不落后于远端最新版本。你仍然可以选择本地 Binary 升级。")
			return s.app.UpgradeFeishu().SimpleStatusCard("已是最新版本", "green", s.app.MenuCardBody("menu.upgrade", strings.Join(bodyLines, "\n")), UpgradePanelButtons(sessionKey, nil, true)), nil
		}
	}

	requestID, err := st.NextLocalID("upgrade")
	if err != nil {
		return nil, err
	}
	payload := UpgradePendingPayload{
		CurrentVersion: current,
		TargetVersion:  target.Version,
		ReleaseTag:     target.ReleaseTag,
		BinaryPath:     exePath,
		DownloadURL:    target.BinaryURL,
		SourceCommit:   target.SourceCommit,
		ExpectedSHA256: target.ExpectedSHA256,
		ReleaseURL:     target.HTMLURL,
	}
	if err := st.SavePending(&state.PendingRequest{
		ID:           requestID,
		RequestIDRaw: requestID,
		Kind:         "upgrade_release",
		SessionKey:   sessionKey,
		OwnerUserID:  ownerUserID,
		PayloadJSON:  mustJSON(payload),
		Status:       state.PendingRequestStatusPending.String(),
		CreatedAt:    time.Now().Unix(),
		ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
	}); err != nil {
		return nil, err
	}
	bodyLines = append(bodyLines, "", RemoteUpgradeSummary(forceVersion, useDevRelease))
	return s.RenderUpgradeConfirmCard("升级确认", sessionKey, requestID, payload, bodyLines), nil
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
			return fmt.Errorf(UpgradeCommandUsage)
		}
		return s.ReplyUpgradeDevCard(msg)
	case "local":
		if len(args) != 1 {
			return fmt.Errorf(UpgradeCommandUsage)
		}
		return s.CommandUpgradeLocalPick(msg)
	case "path":
		if len(args) < 2 {
			return fmt.Errorf(UpgradeCommandUsage)
		}
		return s.CommandUpgradeLocalPath(msg, strings.Join(args[1:], " "))
	}
	if len(args) > 1 {
		return fmt.Errorf(UpgradeCommandUsage)
	}
	targetVersion, err := NormalizeUpgradeVersion(args[0])
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
	_, err = s.app.UpgradeFeishu().ReplyCard(context.Background(), msg.MessageID, card, s.app.ReplyInThreadEnabled(msg.ChatType))
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
	_, err = s.app.UpgradeFeishu().ReplyCard(context.Background(), msg.MessageID, card, s.app.ReplyInThreadEnabled(msg.ChatType))
	return err
}

// ---------------------------------------------------------------------------
// Card action handling
// ---------------------------------------------------------------------------

// CompleteUpgradeAction handles the card action for upgrade confirm/cancel.
func (s UpgradeService) CompleteUpgradeAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	st := s.app.UpgradeState()
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := st.Pending(requestID)
	if pending == nil || pending.Kind != "upgrade_release" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个升级请求"}}, nil
	}
	if actionName == "upgrade.cancel" {
		_ = st.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
		sessionKey, _ := action.ActionValue["session_key"].(string)
		if strings.TrimSpace(sessionKey) == "" {
			sessionKey = pending.SessionKey
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消升级"},
			Card:  rawCard(RenderSystemMenuCard(sessionKey)),
		}, nil
	}

	var payload UpgradePendingPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级参数损坏"}}, nil
	}
	unitName, err := StartDaemonUpgrade(daemon.UpgradeSpec{
		ServiceName:    s.app.DaemonServiceName(),
		Version:        firstNonEmpty(strings.TrimSpace(payload.TargetVersion), firstNonEmpty(strings.TrimSpace(payload.SourceName), "local-artifact")),
		BinaryPath:     payload.BinaryPath,
		DownloadURL:    payload.DownloadURL,
		SourcePath:     payload.SourcePath,
		ExpectedSHA256: payload.ExpectedSHA256,
	})
	if err != nil {
		slog.Error("start daemon upgrade failed",
			"request_id", requestID,
			"target_version", payload.TargetVersion,
			"binary_path", payload.BinaryPath,
			"source_path", payload.SourcePath,
			"error", err,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "启动升级失败，请重试"},
		}, nil
	}
	_ = st.UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusUpgrading.String()
		var p UpgradePendingPayload
		if jsonErr := json.Unmarshal([]byte(req.PayloadJSON), &p); jsonErr == nil {
			p.UnitName = unitName
			p.ChatID = pending.SessionKey // fallback
			p.FeishuMsgID = pending.FeishuMsgID
			if updated, marshalErr := json.Marshal(p); marshalErr == nil {
				req.PayloadJSON = string(updated)
			}
		}
	})
	body := strings.Join([]string{
		UpgradeStartedSummaryLine(payload),
		"后台任务: `" + unitName + "`",
		"服务即将重启；如果启动失败会自动回退。",
	}, "\n")
	sessionKey, _ := action.ActionValue["session_key"].(string)
	if strings.TrimSpace(sessionKey) == "" {
		sessionKey = pending.SessionKey
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已开始升级"},
		Card: rawCard(s.app.UpgradeFeishu().SimpleStatusCard("升级中", "orange", s.app.MenuCardBody("menu.upgrade", body), []feishu.Button{
			{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
		})),
	}, nil
}

// ---------------------------------------------------------------------------
// Runtime validation
// ---------------------------------------------------------------------------

// ValidateUpgradeRuntime checks that the current process is the daemon
// service process and returns the executable path and asset name.
func (s UpgradeService) ValidateUpgradeRuntime() (string, string, error) {
	exePath, assetName, err := s.probeUpgradeRuntime()
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(CurrentGOOS()) != "linux" {
		return "", "", fmt.Errorf("当前平台不支持 daemon 自动升级")
	}
	serviceName := s.app.DaemonServiceName()
	manager, err := NewDaemonManager(serviceName)
	if err != nil {
		return "", "", fmt.Errorf("当前环境不支持 daemon 升级: %w", err)
	}
	status, err := manager.Status()
	if err != nil {
		return "", "", fmt.Errorf("查询 daemon 状态失败: %w", err)
	}
	if status == nil || !status.Installed || !status.Running {
		return "", "", fmt.Errorf("当前 daemon 未安装或未运行")
	}
	if status.PID > 0 && status.PID != os.Getpid() {
		return "", "", fmt.Errorf("当前进程不是 daemon 服务进程，无法执行远程升级")
	}
	return exePath, assetName, nil
}

// probeUpgradeRuntime returns the current executable path and target release
// asset for the active platform without checking daemon availability.
func (s UpgradeService) probeUpgradeRuntime() (string, string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("获取当前二进制路径失败: %w", err)
	}
	if realPath, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = realPath
	}
	assetName, err := release.CurrentAssetName(CurrentGOOS(), CurrentGOARCH())
	if err != nil {
		return "", "", fmt.Errorf("当前平台不支持自动升级: %w", err)
	}
	return exePath, assetName, nil
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
	return s.app.UpgradeFeishu().SimpleStatusCard(title, "orange", s.app.MenuCardBody("menu.upgrade", strings.Join(lines, "\n")), UpgradePanelButtons(sessionKey, map[string]any{
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
			Text: "返回上一级",
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
			Text: "返回上一级",
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

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

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
