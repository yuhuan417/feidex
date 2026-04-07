package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"feidex/internal/state"
)

const attachmentRetention = 7 * 24 * time.Hour
const markdownPreviewRetention = 7 * 24 * time.Hour

func (a *App) expirePendingRequestsOnStartup() {
	for _, req := range a.store.AllPendingRequests() {
		if req == nil || req.Status != "pending" {
			continue
		}
		_ = a.store.UpdatePending(req.ID, func(p *state.PendingRequest) {
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

func (a *App) runMarkdownPreviewGC(source string) {
	if a == nil || a.feishu == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := a.feishu.CleanupMarkdownPreviewsBefore(ctx, time.Now().Add(-markdownPreviewRetention))
	if err != nil {
		slog.Warn("markdown preview gc failed", "source", source, "error", err)
		return
	}
	if result.DeletedFileCount == 0 {
		return
	}
	slog.Info("markdown preview gc complete",
		"source", source,
		"deleted_file_count", result.DeletedFileCount,
		"deleted_estimated_bytes", result.DeletedEstimatedBytes,
	)
}
