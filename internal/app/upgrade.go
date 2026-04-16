package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/release"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	upgradeLocalBinaryPendingKind = "upgrade_local_binary"
	upgradeCommandUsage           = "usage: /upgrade | /upgrade dev | /upgrade [VERSION] | /upgrade local | /upgrade path <PATH>"
)

var upgradeDisplayLocation = time.Local

type upgradePendingPayload struct {
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
}

func (a *App) renderUpgradePreparingCard(sessionKey string) map[string]any {
	body := "正在检查可升级版本，请稍候。\n\n这张卡片会自动刷新。"
	return a.feishu.SimpleStatusCard("升级服务", "blue", menuCardBody("menu.upgrade", body), nil)
}

func (a *App) renderUpgradeFailedCard(sessionKey, errText string) map[string]any {
	body := "检查升级信息失败。"
	if text := strings.TrimSpace(errText); text != "" {
		body += "\n\n错误: " + text
	}
	return a.feishu.SimpleStatusCard("升级服务", "orange", menuCardBody("menu.upgrade", body), upgradePanelButtons(sessionKey, nil, true))
}

func (a *App) renderUpgradeCard(sessionKey, ownerUserID string) (map[string]any, error) {
	return a.renderUpgradeCardForTarget(sessionKey, ownerUserID, "", false)
}

func (a *App) renderUpgradeCardForVersion(sessionKey, ownerUserID, requestedVersion string) (map[string]any, error) {
	return a.renderUpgradeCardForTarget(sessionKey, ownerUserID, requestedVersion, false)
}

func (a *App) renderUpgradeDevCard(sessionKey, ownerUserID string) (map[string]any, error) {
	return a.renderUpgradeCardForTarget(sessionKey, ownerUserID, "", true)
}

func (a *App) renderUpgradeCardForTarget(sessionKey, ownerUserID, requestedVersion string, useDevRelease bool) (map[string]any, error) {
	appState := a.appState()
	exePath, assetName, err := a.validateUpgradeRuntime()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	current := currentVersion()
	bodyLines := []string{
		"当前版本: `" + current + "`",
		"目标架构: `" + currentGOARCH() + "`",
		"目标包: `" + assetName + "`",
		"二进制: `" + exePath + "`",
	}

	target := &release.ReleaseInfo{}
	forceVersion := strings.TrimSpace(requestedVersion) != ""
	switch {
	case useDevRelease:
		target, err = newReleaseClient().LatestDevLinuxBinary(ctx, currentGOARCH())
		if err != nil {
			return nil, fmt.Errorf("查询开发版 %s 失败: %w", release.DevReleaseTag, err)
		}
		bodyLines = append(bodyLines, "开发版本: `"+target.Version+"`")
		if strings.TrimSpace(target.ReleaseTag) != "" && strings.TrimSpace(target.ReleaseTag) != strings.TrimSpace(target.Version) {
			bodyLines = append(bodyLines, "Release Tag: `"+target.ReleaseTag+"`")
		}
		if commit := shortUpgradeCommit(target.SourceCommit); commit != "" {
			bodyLines = append(bodyLines, "提交: `"+commit+"`")
		}
	case forceVersion:
		target, err = newReleaseClient().LinuxBinaryByVersion(ctx, requestedVersion, currentGOARCH())
		if err != nil {
			return nil, fmt.Errorf("查询指定版本 %s 失败: %w", requestedVersion, err)
		}
		bodyLines = append(bodyLines, "指定版本: `"+target.Version+"`")
	default:
		target, err = newReleaseClient().LatestLinuxBinary(ctx, currentGOARCH())
		if err != nil {
			bodyLines = append(bodyLines, "", "远端版本检查失败。你仍然可以选择本地 Binary 升级。", "错误: "+err.Error())
			return a.feishu.SimpleStatusCard("升级服务", "orange", menuCardBody("menu.upgrade", strings.Join(bodyLines, "\n")), upgradePanelButtons(sessionKey, nil, true)), nil
		}
		bodyLines = append(bodyLines, "最新版本: `"+target.Version+"`")
	}
	if published := formatUpgradeReleasePublishedAt(target.PublishedAt); published != "" {
		bodyLines = append(bodyLines, "发布时间(本机时区): `"+published+"`")
	}
	if strings.TrimSpace(target.HTMLURL) != "" {
		bodyLines = append(bodyLines, "Release: <"+target.HTMLURL+">")
	}

	if !forceVersion && !useDevRelease {
		if cmp, cmpErr := release.CompareVersions(current, target.Version); cmpErr == nil && cmp >= 0 {
			bodyLines = append(bodyLines, "", "当前版本已不落后于远端最新版本。你仍然可以选择本地 Binary 升级。")
			return a.feishu.SimpleStatusCard("已是最新版本", "green", menuCardBody("menu.upgrade", strings.Join(bodyLines, "\n")), upgradePanelButtons(sessionKey, nil, true)), nil
		}
	}

	requestID, err := appState.nextLocalID("upgrade")
	if err != nil {
		return nil, err
	}
	payload := upgradePendingPayload{
		CurrentVersion: current,
		TargetVersion:  target.Version,
		ReleaseTag:     target.ReleaseTag,
		BinaryPath:     exePath,
		DownloadURL:    target.BinaryURL,
		SourceCommit:   target.SourceCommit,
		ExpectedSHA256: target.ExpectedSHA256,
		ReleaseURL:     target.HTMLURL,
	}
	if err := appState.savePending(&state.PendingRequest{
		ID:           requestID,
		RequestIDRaw: requestID,
		Kind:         "upgrade_release",
		SessionKey:   sessionKey,
		OwnerUserID:  ownerUserID,
		PayloadJSON:  mustJSON(payload),
		Status:       "pending",
		CreatedAt:    time.Now().Unix(),
		ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
	}); err != nil {
		return nil, err
	}
	bodyLines = append(bodyLines, "", remoteUpgradeSummary(forceVersion, useDevRelease))
	return a.renderUpgradeConfirmCard("升级确认", sessionKey, requestID, payload, bodyLines), nil
}

func (a *App) commandUpgrade(msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		return a.replyUpgradeCard(msg, "")
	}
	switch strings.TrimSpace(args[0]) {
	case "dev":
		if len(args) != 1 {
			return fmt.Errorf(upgradeCommandUsage)
		}
		return a.replyUpgradeDevCard(msg)
	case "local":
		if len(args) != 1 {
			return fmt.Errorf(upgradeCommandUsage)
		}
		return a.commandUpgradeLocalPick(msg)
	case "path":
		if len(args) < 2 {
			return fmt.Errorf(upgradeCommandUsage)
		}
		return a.commandUpgradeLocalPath(msg, strings.Join(args[1:], " "))
	}
	if len(args) > 1 {
		return fmt.Errorf(upgradeCommandUsage)
	}
	targetVersion, err := normalizeUpgradeVersion(args[0])
	if err != nil {
		return fmt.Errorf("版本格式不正确: %q，示例: /upgrade v0.3.0", args[0])
	}
	return a.replyUpgradeCard(msg, targetVersion)
}

func (a *App) replyUpgradeCard(msg *feishu.InboundMessage, targetVersion string) error {
	if msg == nil {
		return nil
	}
	card, err := a.renderUpgradeCardForVersion(a.makeSessionKey(msg), msg.UserID, targetVersion)
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) replyUpgradeDevCard(msg *feishu.InboundMessage) error {
	if msg == nil {
		return nil
	}
	card, err := a.renderUpgradeDevCard(a.makeSessionKey(msg), msg.UserID)
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) commandUpgradeLocalPick(msg *feishu.InboundMessage) error {
	if msg == nil {
		return nil
	}
	sessionKey, _, ws := a.currentWorkspaceForMessage(msg)
	requestID, payload, err := a.createUpgradeLocalPickerRequest(sessionKey, ws, msg.UserID, "")
	if err != nil {
		return err
	}
	card, err := a.renderPathPickerCard(requestID, payload)
	if err != nil {
		return err
	}
	msgID, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	if err != nil {
		return err
	}
	return a.appState().updatePending(requestID, func(req *state.PendingRequest) {
		req.FeishuMsgID = msgID
	})
}

func (a *App) commandUpgradeLocalPath(msg *feishu.InboundMessage, rawPath string) error {
	if msg == nil {
		return nil
	}
	sessionKey, _, ws := a.currentWorkspaceForMessage(msg)
	selectedPath, err := resolveUpgradeLocalSourcePath(ws, rawPath)
	if err != nil {
		return err
	}
	requestID, payload, err := a.createLocalUpgradeRequest(sessionKey, msg.UserID, "", selectedPath)
	if err != nil {
		return err
	}
	card := a.renderUpgradeConfirmCard("升级确认", sessionKey, requestID, payload, upgradeLocalConfirmLines(payload.BinaryPath))
	msgID, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	if err != nil {
		return err
	}
	return a.appState().updatePending(requestID, func(req *state.PendingRequest) {
		req.FeishuMsgID = msgID
	})
}

func resolveUpgradeLocalSourcePath(ws *config.Workspace, rawPath string) (string, error) {
	root, err := resolvePathPickerRoot(ws)
	if err != nil {
		return "", err
	}
	resolved, err := resolvePathPickerPath(root, rawPath)
	if err != nil {
		return "", fmt.Errorf("解析本地 Binary 路径失败: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("读取本地 Binary 失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("本地 Binary 必须是普通文件")
	}
	return resolved, nil
}

func (a *App) createUpgradeLocalPickerRequest(sessionKey string, ws *config.Workspace, ownerUserID, feishuMsgID string) (string, pathPickerPayload, error) {
	if _, _, err := a.validateUpgradeRuntime(); err != nil {
		return "", pathPickerPayload{}, err
	}
	appState := a.appState()
	payload, err := a.newDownloadPathPickerPayload(ws)
	if err != nil {
		return "", pathPickerPayload{}, err
	}
	requestID, err := appState.nextLocalID("upgrade-local")
	if err != nil {
		return "", pathPickerPayload{}, err
	}
	if err := appState.savePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        upgradeLocalBinaryPendingKind,
		SessionKey:  sessionKey,
		OwnerUserID: ownerUserID,
		FeishuMsgID: feishuMsgID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	}); err != nil {
		return "", pathPickerPayload{}, err
	}
	return requestID, payload, nil
}

func (a *App) createLocalUpgradeRequest(sessionKey, ownerUserID, feishuMsgID, selectedPath string) (string, upgradePendingPayload, error) {
	exePath, _, err := a.validateUpgradeRuntime()
	if err != nil {
		return "", upgradePendingPayload{}, err
	}
	appState := a.appState()
	requestID, err := appState.nextLocalID("upgrade")
	if err != nil {
		return "", upgradePendingPayload{}, err
	}
	stagedPath, sha256Hex, sizeBytes, err := a.stageLocalUpgradeArtifact(requestID, selectedPath)
	if err != nil {
		return "", upgradePendingPayload{}, err
	}
	payload := upgradePendingPayload{
		CurrentVersion: currentVersion(),
		TargetVersion:  filepath.Base(selectedPath),
		BinaryPath:     exePath,
		SourcePath:     stagedPath,
		SourceKind:     "local_file",
		SourceName:     filepath.Base(selectedPath),
		SourceSize:     sizeBytes,
		ExpectedSHA256: sha256Hex,
	}
	if err := appState.savePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "upgrade_release",
		SessionKey:  sessionKey,
		OwnerUserID: ownerUserID,
		FeishuMsgID: feishuMsgID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(30 * time.Minute).Unix(),
	}); err != nil {
		return "", upgradePendingPayload{}, err
	}
	return requestID, payload, nil
}

func normalizeUpgradeVersion(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("missing version")
	}
	if _, err := release.ParseVersion(raw); err != nil {
		return "", err
	}
	return "v" + strings.TrimPrefix(raw, "v"), nil
}

func (a *App) completeUpgradeAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := appState.pending(requestID)
	if pending == nil || pending.Kind != "upgrade_release" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个升级请求"}}, nil
	}
	if actionName == "upgrade.cancel" {
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
		sessionKey, _ := action.ActionValue["session_key"].(string)
		if strings.TrimSpace(sessionKey) == "" {
			sessionKey = pending.SessionKey
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "success", Content: "已取消升级"},
			Card:  rawCard(a.renderSystemMenuCard(sessionKey)),
		}, nil
	}

	var payload upgradePendingPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级参数损坏"}}, nil
	}
	unitName, err := startDaemonUpgrade(daemon.UpgradeSpec{
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
	_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	body := strings.Join([]string{
		upgradeStartedSummaryLine(payload),
		"后台任务: `" + unitName + "`",
		"服务即将重启；如果启动失败会自动回退。",
	}, "\n")
	sessionKey, _ := action.ActionValue["session_key"].(string)
	if strings.TrimSpace(sessionKey) == "" {
		sessionKey = pending.SessionKey
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已开始升级"},
		Card: rawCard(a.feishu.SimpleStatusCard("升级中", "orange", menuCardBody("menu.upgrade", body), []feishu.Button{
			{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
		})),
	}, nil
}

func (a *App) completeUpgradeLocalPick(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	appState := a.appState()
	wsID := a.defaultWorkspaceID()
	if sess := appState.session(sessionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		wsID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(a.cfg, wsID)
	requestID, payload, err := a.createUpgradeLocalPickerRequest(sessionKey, ws, action.UserID, action.MessageID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	card, err := a.renderPathPickerCard(requestID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "请选择本地 Binary"},
		Card:  rawCard(card),
	}, nil
}

func (a *App) completeUpgradeLocalBinaryConfirm(action *feishu.CardAction, pending *state.PendingRequest, payload pathPickerPayload, selectedPath string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := firstNonEmpty(strings.TrimSpace(pending.SessionKey), actionSessionKey(action))
	requestID, upgradePayload, err := a.createLocalUpgradeRequest(sessionKey, pending.OwnerUserID, firstNonEmpty(strings.TrimSpace(pending.FeishuMsgID), strings.TrimSpace(action.MessageID)), selectedPath)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	_ = a.appState().updatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已选择本地 Binary"},
		Card:  rawCard(a.renderUpgradeConfirmCard("升级确认", sessionKey, requestID, upgradePayload, upgradeLocalConfirmLines(upgradePayload.BinaryPath))),
	}, nil
}

func (a *App) validateUpgradeRuntime() (string, string, error) {
	manager, err := newDaemonManager()
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
	exePath, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("获取当前二进制路径失败: %w", err)
	}
	if realPath, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = realPath
	}
	assetName, err := release.CurrentLinuxAssetName(currentGOARCH())
	if err != nil {
		return "", "", fmt.Errorf("当前架构不支持自动升级: %w", err)
	}
	return exePath, assetName, nil
}

func remoteUpgradeSummary(forceVersion, useDevRelease bool) string {
	if useDevRelease {
		return "确认后会下载 `dev-latest` 当前指向的开发版构建、重启 daemon；如果启动失败会自动回退到旧版本。"
	}
	if forceVersion {
		return "已跳过最新版本检查。确认后会下载指定版本、重启 daemon；如果启动失败会自动回退到旧版本。"
	}
	return "确认后会下载新版本、重启 daemon；如果启动失败会自动回退到旧版本。"
}

func upgradePanelButtons(sessionKey string, confirm map[string]any, includeBack bool) []feishu.Button {
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

func (a *App) renderUpgradeConfirmCard(title, sessionKey, requestID string, payload upgradePendingPayload, lines []string) map[string]any {
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
			lines = append(lines, "大小: `"+formatDownloadSize(payload.SourceSize)+"`")
		}
		lines = append(lines,
			"sha256: `"+strings.TrimSpace(payload.ExpectedSHA256)+"`",
			"",
			"确认后会使用本地制品重启 daemon；如果启动失败会自动回退到旧版本。",
		)
	}
	return a.feishu.SimpleStatusCard(title, "orange", menuCardBody("menu.upgrade", strings.Join(lines, "\n")), upgradePanelButtons(sessionKey, map[string]any{
		"request_id": requestID,
		"label":      buttonLabel,
	}, true))
}

func (a *App) stageLocalUpgradeArtifact(requestID, sourcePath string) (string, string, int64, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return "", "", 0, fmt.Errorf("missing local binary path")
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", "", 0, fmt.Errorf("读取本地制品失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", "", 0, fmt.Errorf("本地制品不是普通文件")
	}
	dir := filepath.Join(a.cfg.DataDir, "upgrades", requestID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", 0, err
	}
	targetPath := filepath.Join(dir, filepath.Base(sourcePath))
	if filepath.Clean(targetPath) == filepath.Clean(sourcePath) {
		targetPath = filepath.Join(dir, "artifact-"+filepath.Base(sourcePath))
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return "", "", 0, err
	}
	defer source.Close()
	target, err := os.Create(targetPath)
	if err != nil {
		return "", "", 0, err
	}
	defer target.Close()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(target, hash), source)
	if err != nil {
		return "", "", 0, err
	}
	if err := target.Chmod(0o755); err != nil {
		return "", "", 0, err
	}
	return targetPath, hex.EncodeToString(hash.Sum(nil)), written, nil
}

func upgradeLocalConfirmLines(binaryPath string) []string {
	return []string{
		"当前版本: `" + currentVersion() + "`",
		"目标架构: `" + currentGOARCH() + "`",
		"二进制: `" + binaryPath + "`",
	}
}

func upgradeStartedSummaryLine(payload upgradePendingPayload) string {
	if strings.TrimSpace(payload.SourcePath) != "" {
		return "本地制品: `" + firstNonEmpty(strings.TrimSpace(payload.SourceName), filepath.Base(payload.SourcePath)) + "`"
	}
	line := "目标版本: `" + payload.TargetVersion + "`"
	if tag := strings.TrimSpace(payload.ReleaseTag); tag != "" && tag != strings.TrimSpace(payload.TargetVersion) {
		line += "\nRelease Tag: `" + tag + "`"
	}
	if commit := shortUpgradeCommit(payload.SourceCommit); commit != "" {
		line += "\n提交: `" + commit + "`"
	}
	return line
}

func shortUpgradeCommit(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func formatUpgradeReleasePublishedAt(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.In(upgradeDisplayLocation).Format("2006-01-02 15:04:05")
}
