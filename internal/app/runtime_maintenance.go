package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type runtimeMaintenanceService struct {
	app *App
}
func newRuntimeMaintenanceService(app *App) runtimeMaintenanceService {
	return runtimeMaintenanceService{app: app}
}

const attachmentRetention = 7 * 24 * time.Hour
const artifactRetention = 3 * 24 * time.Hour

type permissionIssueDiagnosticSender interface {
	notifyPermissionIssue(feishuNotifyTarget, error)
}

func (s runtimeMaintenanceService) expirePendingRequestsOnStartup() {
	if s.app == nil || s.app.store == nil {
		return
	}
	for _, req := range s.app.store.AllPendingRequests() {
		if req == nil || (req.Status != "pending" && req.Status != "replied") {
			continue
		}
		_ = s.app.store.UpdateScopedPending(req.FrontendID, req.ID, func(p *state.PendingRequest) {
			p.Status = "expired"
			if p.ExpiresAt < time.Now().Unix() {
				return
			}
			p.ExpiresAt = time.Now().Unix()
		})
	}
}

func (s runtimeMaintenanceService) cleanupExpiredAttachments() {
	for _, ws := range s.app.cfg.Workspaces {
		root := filepath.Join(ws.Cwd, attachmentsDirName)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			newRuntimeMaintenanceService(s.app).cleanupAttachmentDir(filepath.Join(root, entry.Name()))
		}
	}
}

func (s runtimeMaintenanceService) cleanupAttachmentDir(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	threshold := time.Now().Add(-attachmentRetention)
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(threshold) {
			_ = os.RemoveAll(path)
		}
	}
}

func (s runtimeMaintenanceService) cleanupSubmissionRuntimeState(sub *state.Submission) {
	if s.app == nil || s.app.store == nil || sub == nil {
		return
	}
	appState := appState(s.app)
	submissionID := strings.TrimSpace(sub.ID)
	turnID := strings.TrimSpace(sub.TurnID)
	threadID := strings.TrimSpace(sub.ThreadID)
	appState.deleteMessageLinks(func(link *state.MessageLink) bool {
		if link == nil {
			return false
		}
		if submissionID != "" && strings.TrimSpace(link.SubmissionID) == submissionID {
			return true
		}
		if turnID != "" && strings.TrimSpace(link.TurnID) == turnID {
			return true
		}
		return false
	})
	if turnID != "" {
		appState.deletePendingRequests(func(req *state.PendingRequest) bool {
			return req != nil && strings.TrimSpace(req.TurnID) == turnID
		})
	}
	if submissionID != "" {
		appState.deleteSubmission(submissionID)
	}
	if turnID != "" {
		newRuntimeStateService(s.app).clearTurnBinding(turnID)
		newRuntimeStateService(s.app).clearTurnItemStates(turnID)
	}
	if submissionID != "" && threadID != "" {
		newRuntimeStateService(s.app).clearPendingTurnBindingForSubmission(threadID, submissionID)
	}
}

func (s runtimeMaintenanceService) startDriveArtifactGCLoop(ctx context.Context) {
	if s.app == nil || s.app.feishu == nil {
		return
	}
	go newRuntimeMaintenanceService(s.app).runDriveArtifactGC("startup")
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				newRuntimeMaintenanceService(s.app).runDriveArtifactGC("ticker")
			}
		}
	}()
}

func (s runtimeMaintenanceService) startUpgradeCheckLoop(ctx context.Context) {
	if s.app == nil || s.app.feishu == nil {
		return
	}
	go newRuntimeMaintenanceService(s.app).checkPendingUpgrades("startup")
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				newRuntimeMaintenanceService(s.app).checkPendingUpgrades("ticker")
			}
		}
	}()
}

func (s runtimeMaintenanceService) checkPendingUpgrades(source string) {
	if s.app == nil || s.app.store == nil {
		return
	}
	pendings := appState(s.app).pendingRequests()
	for _, pending := range pendings {
		if pending != nil && pending.Status == "upgrading" {
			s.checkOneUpgrade(source, pending)
		}
	}
}

func (s runtimeMaintenanceService) checkOneUpgrade(source string, pending *state.PendingRequest) {
	if pending == nil {
		return
	}
	var payload upgradePendingPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		slog.Warn("upgrade check: bad payload", "request_id", pending.ID, "error", err)
		appState(s.app).updatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
		return
	}
	unitName := strings.TrimSpace(payload.UnitName)
	if unitName == "" {
		slog.Warn("upgrade check: missing unit name", "request_id", pending.ID)
		appState(s.app).updatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
		return
	}

	st, err := daemon.QueryUpgradeUnitStatus(unitName)
	if err != nil {
		slog.Debug("upgrade check: query failed", "unit", unitName, "error", err)
		return // transient, retry next tick
	}
	if st == nil {
		// unit not found (collected or never existed)
		slog.Warn("upgrade check: unit not found, marking resolved", "unit", unitName, "source", source)
		appState(s.app).updatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
		return
	}
	if st.ActiveState == "active" || st.ActiveState == "activating" {
		return // still running
	}

	// Unit has exited — patch card and clean up
	appState(s.app).updatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
	daemon.CleanupUpgradeUnit(unitName)

	sessionKey := strings.TrimSpace(pending.SessionKey)
	if sessionKey == "" {
		sessionKey = payload.ChatID
	}
	feishuMsgID := strings.TrimSpace(pending.FeishuMsgID)
	if feishuMsgID == "" {
		feishuMsgID = payload.FeishuMsgID
	}
	if feishuMsgID == "" {
		slog.Warn("upgrade check: no feishu msg id to patch", "unit", unitName, "request_id", pending.ID)
		return
	}

	var card map[string]any
	if st.Result == "success" {
		slog.Info("upgrade unit succeeded", "unit", unitName, "source", source)
		body := "升级已完成，服务已重启。"
		card = s.app.feishu.SimpleStatusCard("升级成功", "green", menuCardBody("menu.upgrade", body), []feishu.Button{
			{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
		})
	} else {
		errMsg := extractUpgradeErrorFromJournal(st.JournalTail)
		slog.Warn("upgrade unit failed", "unit", unitName, "result", st.Result, "error", errMsg, "source", source)
		body := "升级失败。"
		if errMsg != "" {
			body += "\n\n错误: " + errMsg
		}
		card = s.app.feishu.SimpleStatusCard("升级失败", "red", menuCardBody("menu.upgrade", body), []feishu.Button{
			{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.app.feishu.PatchCard(ctx, feishuMsgID, card); err != nil {
		slog.Error("upgrade check: patch card failed", "unit", unitName, "msg_id", feishuMsgID, "error", err)
	}
}

func extractUpgradeErrorFromJournal(journal string) string {
	lines := strings.Split(journal, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.Contains(line, "error") || strings.Contains(line, "Error") || strings.Contains(line, "failed") || strings.Contains(line, "mismatch") {
			return line
		}
	}
	// fallback: last non-empty line
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.TrimSpace(lines[i])
		}
	}
	return ""
}

func (s runtimeMaintenanceService) runDriveArtifactGC(source string) {
	if s.app == nil || s.app.feishu == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := s.app.feishu.CleanupArtifactsBefore(ctx, time.Now().Add(-artifactRetention))
	if err != nil {
		newRuntimeMaintenanceService(s.app).notifyDriveArtifactGCPermissionIssue(source, err)
		slog.Warn("artifact gc failed", "source", source, "error", err)
		return
	}
	if result.DeletedFileCount == 0 {
		return
	}
	slog.Debug("artifact gc complete",
		"source", source,
		"deleted_file_count", result.DeletedFileCount,
		"deleted_estimated_bytes", result.DeletedEstimatedBytes,
	)
}

func (s runtimeMaintenanceService) notifyDriveArtifactGCPermissionIssue(source string, err error) {
	if s.app == nil || s.app.feishu == nil || err == nil {
		return
	}
	issue, ok := feishu.PermissionIssueFromError(err)
	if !ok || issue == nil {
		return
	}
	body := renderFeishuPermissionIssueBody(issue)
	if body == "" {
		return
	}
	notifier, ok := s.app.feishu.(permissionIssueDiagnosticSender)
	chatIDs := appStartupReadyChatIDs(s.app, appState(s.app).sessions())
	if len(chatIDs) == 0 {
		queueFrontendCardNotification(s.app, state.FrontendCardNotification{
			Kind:        frontendCardNotificationKindFeishuPermissionIssue,
			CollapseKey: frontendCardNotificationKindFeishuPermissionIssue,
			Title:       "飞书权限错误",
			Color:       "red",
			Body:        body,
		})
		slog.Debug("artifact gc permission diagnostic queued",
			"source", source,
			"reason", "no_known_chats",
			"api", strings.TrimSpace(issue.API),
		)
		return
	}
	if !ok {
		queueFrontendCardNotification(s.app, state.FrontendCardNotification{
			Kind:        frontendCardNotificationKindFeishuPermissionIssue,
			CollapseKey: frontendCardNotificationKindFeishuPermissionIssue,
			Title:       "飞书权限错误",
			Color:       "red",
			Body:        body,
		})
		slog.Debug("artifact gc permission diagnostic queued",
			"source", source,
			"reason", "feishu_notifier_unavailable",
			"api", strings.TrimSpace(issue.API),
		)
		return
	}
	for _, chatID := range chatIDs {
		notifier.notifyPermissionIssue(feishuNotifyTarget{ChatID: chatID}, err)
	}
}
