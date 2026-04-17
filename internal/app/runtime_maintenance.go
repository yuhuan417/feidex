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
const codexAppServerGCInterval = time.Minute

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

func (a *App) startMarkdownPreviewGCLoop(ctx context.Context) {
	if a == nil || a.feishu == nil {
		return
	}
	go a.runMarkdownPreviewGC("startup")
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.runMarkdownPreviewGC("ticker")
			}
		}
	}()
}

func (a *App) startCodexAppServerGCLoop(ctx context.Context) {
	if a == nil || a.codexPool == nil {
		return
	}
	ttl, err := time.ParseDuration(strings.TrimSpace(a.cfg.Codex.AppServerIdleTTL))
	if err != nil || ttl <= 0 {
		return
	}
	go a.runCodexAppServerGC("startup", ttl)
	go func() {
		ticker := time.NewTicker(codexAppServerGCInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.runCodexAppServerGC("ticker", ttl)
			}
		}
	}()
}

func (a *App) runCodexAppServerGC(source string, ttl time.Duration) {
	if a == nil || a.codexPool == nil || ttl <= 0 {
		return
	}
	closed := a.codexPool.CloseIdleClients(time.Now(), ttl, a.busyCodexWorkspaceIDs())
	if closed == 0 {
		return
	}
	slog.Debug("codex app-server gc complete",
		"source", source,
		"closed_client_count", closed,
		"idle_ttl", ttl.String(),
	)
}

func (a *App) busyCodexWorkspaceIDs() map[string]struct{} {
	busy := map[string]struct{}{}
	if a == nil || a.store == nil {
		return busy
	}
	appState := a.appState()
	for _, sess := range appState.sessions() {
		if sess == nil || !sessionHasActiveWork(sess) {
			continue
		}
		workspaceID := strings.TrimSpace(firstNonEmpty(sess.ActiveThreadWorkspaceID, sess.WorkspaceID))
		if workspaceID == "" {
			workspaceID = a.defaultWorkspaceID()
		}
		busy[workspaceID] = struct{}{}
	}
	return busy
}

func (a *App) runMarkdownPreviewGC(source string) {
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
