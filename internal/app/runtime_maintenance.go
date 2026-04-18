package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/state"
)

const attachmentRetention = 7 * 24 * time.Hour
const artifactRetention = 3 * 24 * time.Hour

func (a *App) expirePendingRequestsOnStartup() {
	appState := a.appState()
	for _, req := range appState.pendingRequests() {
		if req == nil || (req.Status != "pending" && req.Status != "replied") {
			continue
		}
		_ = appState.updatePending(req.ID, func(p *state.PendingRequest) {
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
		a.clearTurnBinding(turnID)
		a.clearTurnItemStates(turnID)
	}
	if threadID != "" {
		a.clearPendingTurnBinding(threadID)
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
