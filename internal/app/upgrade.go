package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/release"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type upgradePendingPayload struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	BinaryPath     string `json:"binary_path"`
	DownloadURL    string `json:"download_url"`
	ExpectedSHA256 string `json:"expected_sha256"`
	ReleaseURL     string `json:"release_url"`
}

func (a *App) renderUpgradeCard(sessionKey, ownerUserID string) (map[string]any, error) {
	manager, err := newDaemonManager()
	if err != nil {
		return nil, fmt.Errorf("当前环境不支持 daemon 升级: %w", err)
	}
	status, err := manager.Status()
	if err != nil {
		return nil, fmt.Errorf("查询 daemon 状态失败: %w", err)
	}
	if status == nil || !status.Installed || !status.Running {
		return nil, fmt.Errorf("当前 daemon 未安装或未运行")
	}
	if status.PID > 0 && status.PID != os.Getpid() {
		return nil, fmt.Errorf("当前进程不是 daemon 服务进程，无法执行远程升级")
	}

	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("获取当前二进制路径失败: %w", err)
	}
	if realPath, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = realPath
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	latest, err := newReleaseClient().LatestLinuxAMD64(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询最新版本失败: %w", err)
	}

	current := currentVersion()
	bodyLines := []string{
		"当前版本: `" + current + "`",
		"最新版本: `" + latest.Version + "`",
		"二进制: `" + exePath + "`",
	}
	if strings.TrimSpace(latest.HTMLURL) != "" {
		bodyLines = append(bodyLines, "Release: <"+latest.HTMLURL+">")
	}

	if cmp, err := release.CompareVersions(current, latest.Version); err == nil && cmp >= 0 {
		return a.feishu.SimpleStatusCard("已是最新版本", "green", menuCardBody("menu.upgrade", strings.Join(bodyLines, "\n")), []feishu.Button{
			{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
		}), nil
	}

	requestID, err := a.store.NextLocalID("upgrade")
	if err != nil {
		return nil, err
	}
	payload := upgradePendingPayload{
		CurrentVersion: current,
		TargetVersion:  latest.Version,
		BinaryPath:     exePath,
		DownloadURL:    latest.BinaryURL,
		ExpectedSHA256: latest.ExpectedSHA256,
		ReleaseURL:     latest.HTMLURL,
	}
	bodyLines = append(bodyLines, "", "确认后会下载新版本、重启 daemon；如果启动失败会自动回退到旧版本。")
	card := a.feishu.SimpleStatusCard("升级确认", "orange", menuCardBody("menu.upgrade", strings.Join(bodyLines, "\n")), []feishu.Button{
		{Text: "升级到 " + latest.Version, Type: "primary", Value: map[string]any{"action": "upgrade.confirm", "request_id": requestID, "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "upgrade.cancel", "request_id": requestID, "session_key": sessionKey}},
	})
	if err := a.store.UpsertPending(&state.PendingRequest{
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
	return card, nil
}

func (a *App) commandUpgrade(msg *feishu.InboundMessage) error {
	card, err := a.renderUpgradeCard(a.makeSessionKey(msg), msg.UserID)
	if err != nil {
		return err
	}
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) completeUpgradeAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := a.store.PendingByID(requestID)
	if pending == nil || pending.Kind != "upgrade_release" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "升级请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个升级请求"}}, nil
	}
	if actionName == "upgrade.cancel" {
		_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
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
		Version:        payload.TargetVersion,
		BinaryPath:     payload.BinaryPath,
		DownloadURL:    payload.DownloadURL,
		ExpectedSHA256: payload.ExpectedSHA256,
	})
	if err != nil {
		slog.Error("start daemon upgrade failed",
			"request_id", requestID,
			"target_version", payload.TargetVersion,
			"binary_path", payload.BinaryPath,
			"error", err,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "启动升级失败，请重试"},
		}, nil
	}
	_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	body := "目标版本: `" + payload.TargetVersion + "`\n后台任务: `" + unitName + "`\n服务即将重启；如果启动失败会自动回退。"
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
