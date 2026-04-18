package feishu

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

type SharedFileRequest struct {
	LocalPath string
	ChatID    string
	UserID    string
}

type SharedFileResult struct {
	FileName  string
	URL       string
	SizeBytes int64
}

type FileShareConfig struct {
	RootFolderName string
	MaxFileBytes   int64
}

type DriveFileSharer struct {
	store *DriveArtifactStore
}

func NewDriveFileSharer(api previewDriveAPI, cfg FileShareConfig) *DriveFileSharer {
	rootFolderName := strings.TrimSpace(cfg.RootFolderName)
	if rootFolderName == "" {
		rootFolderName = defaultArtifactRootFolderName
	}
	return &DriveFileSharer{store: NewDriveArtifactStore(api, ArtifactStoreConfig{
		RootFolderName: rootFolderName,
		MaxFileBytes:   cfg.MaxFileBytes,
	})}
}

func (a *Adapter) ShareLocalFile(ctx context.Context, req SharedFileRequest) (SharedFileResult, error) {
	store := a.ensureDriveArtifactStore()
	if store == nil {
		return SharedFileResult{}, fmt.Errorf("drive artifact store is not available")
	}
	result, err := store.UploadLocalFile(ctx, ArtifactUploadRequest{
		LocalPath: req.LocalPath,
		ChatID:    req.ChatID,
		UserID:    req.UserID,
	})
	if err != nil {
		return SharedFileResult{}, err
	}
	return SharedFileResult{
		FileName:  result.FileName,
		URL:       result.URL,
		SizeBytes: result.SizeBytes,
	}, nil
}

func (a *Adapter) CleanupArtifactsBefore(ctx context.Context, cutoff time.Time) (PreviewDriveCleanupResult, error) {
	store := a.ensureDriveArtifactStore()
	if store == nil {
		return PreviewDriveCleanupResult{}, nil
	}
	return store.CleanupBefore(ctx, cutoff)
}

func (a *Adapter) ensureDriveArtifactStore() *DriveArtifactStore {
	a.artifactMu.Lock()
	defer a.artifactMu.Unlock()
	if a.client == nil {
		return nil
	}
	if a.artifactStore != nil {
		return a.artifactStore
	}
	a.artifactStore = NewDriveArtifactStore(NewLarkDrivePreviewAPI(a.client), ArtifactStoreConfig{
		RootFolderName: defaultArtifactRootFolderName,
	})
	return a.artifactStore
}

func (s *DriveFileSharer) ShareFile(ctx context.Context, req SharedFileRequest) (SharedFileResult, error) {
	if s == nil || s.store == nil {
		return SharedFileResult{}, fmt.Errorf("drive artifact store is not available")
	}
	result, err := s.store.UploadLocalFile(ctx, ArtifactUploadRequest{
		LocalPath: req.LocalPath,
		ChatID:    req.ChatID,
		UserID:    req.UserID,
	})
	if err != nil {
		return SharedFileResult{}, err
	}
	return SharedFileResult{
		FileName:  result.FileName,
		URL:       result.URL,
		SizeBytes: result.SizeBytes,
	}, nil
}

func (s *DriveFileSharer) CleanupBefore(ctx context.Context, cutoff time.Time) (PreviewDriveCleanupResult, error) {
	if s == nil || s.store == nil {
		return PreviewDriveCleanupResult{}, nil
	}
	return s.store.CleanupBefore(ctx, cutoff)
}

func validateSharedLocalFile(localPath string, maxBytes int64) (string, os.FileInfo, error) {
	return validateLocalArtifactFile(localPath, maxBytes)
}
