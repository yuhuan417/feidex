package app

import (
	"os"
	"path/filepath"
	"time"

	"feidex/internal/state"
)

const attachmentRetention = 7 * 24 * time.Hour

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
