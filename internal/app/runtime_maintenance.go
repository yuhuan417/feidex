package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

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
	chatIDs := s.app.startupReadyChatIDs(appState(s.app).sessions())
	if len(chatIDs) == 0 {
		s.app.queueFrontendCardNotification(state.FrontendCardNotification{
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
		s.app.queueFrontendCardNotification(state.FrontendCardNotification{
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
