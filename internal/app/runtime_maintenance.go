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

func (a *App) expirePendingRequestsOnStartup() {
	if a == nil || a.store == nil {
		return
	}
	for _, req := range a.store.AllPendingRequests() {
		if req == nil || (req.Status != "pending" && req.Status != "replied") {
			continue
		}
		_ = a.store.UpdateScopedPending(req.FrontendID, req.ID, func(p *state.PendingRequest) {
			p.Status = "expired"
			if p.ExpiresAt < time.Now().Unix() {
				return
			}
			p.ExpiresAt = time.Now().Unix()
		})
	}
}

func (a *App) cleanupExpiredAttachments() {
	for _, ws := range a.cfg.Workspaces {
		root := filepath.Join(ws.Cwd, attachmentsDirName)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			a.cleanupAttachmentDir(filepath.Join(root, entry.Name()))
		}
	}
}

func (a *App) cleanupAttachmentDir(root string) {
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

func (a *App) cleanupSubmissionRuntimeState(sub *state.Submission) {
	if a == nil || a.store == nil || sub == nil {
		return
	}
	appState := a.appState()
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
		newRuntimeStateService(a).clearTurnBinding(turnID)
		newRuntimeStateService(a).clearTurnItemStates(turnID)
	}
	if submissionID != "" && threadID != "" {
		newRuntimeStateService(a).clearPendingTurnBindingForSubmission(threadID, submissionID)
	}
}

func (a *App) startDriveArtifactGCLoop(ctx context.Context) {
	if a == nil || a.feishu == nil {
		return
	}
	go a.runDriveArtifactGC("startup")
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.runDriveArtifactGC("ticker")
			}
		}
	}()
}

func (a *App) runDriveArtifactGC(source string) {
	if a == nil || a.feishu == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := a.feishu.CleanupArtifactsBefore(ctx, time.Now().Add(-artifactRetention))
	if err != nil {
		a.notifyDriveArtifactGCPermissionIssue(source, err)
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

func (a *App) notifyDriveArtifactGCPermissionIssue(source string, err error) {
	if a == nil || a.feishu == nil || err == nil {
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
	notifier, ok := a.feishu.(permissionIssueDiagnosticSender)
	chatIDs := a.startupReadyChatIDs(a.appState().sessions())
	if len(chatIDs) == 0 {
		a.queueFrontendCardNotification(state.FrontendCardNotification{
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
		a.queueFrontendCardNotification(state.FrontendCardNotification{
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
