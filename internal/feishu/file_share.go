package feishu

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultFileShareRootFolderName = "Feidex Downloads"
	defaultFileShareMaxFileBytes   = 64 * 1024 * 1024
	fileShareManagedFilePrefix     = "fxdl-"
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
	api    previewDriveAPI
	config FileShareConfig

	mu   sync.Mutex
	root *previewFolderRecord
}

func NewDriveFileSharer(api previewDriveAPI, cfg FileShareConfig) *DriveFileSharer {
	if cfg.RootFolderName == "" {
		cfg.RootFolderName = defaultFileShareRootFolderName
	}
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = defaultFileShareMaxFileBytes
	}
	return &DriveFileSharer{api: api, config: cfg}
}

func (a *Adapter) ShareLocalFile(ctx context.Context, req SharedFileRequest) (SharedFileResult, error) {
	sharer := a.ensureDriveFileSharer()
	if sharer == nil {
		return SharedFileResult{}, fmt.Errorf("drive file sharer is not available")
	}
	return sharer.ShareFile(ctx, req)
}

func (a *Adapter) CleanupSharedFilesBefore(ctx context.Context, cutoff time.Time) (PreviewDriveCleanupResult, error) {
	sharer := a.ensureDriveFileSharer()
	if sharer == nil {
		return PreviewDriveCleanupResult{}, nil
	}
	return sharer.CleanupBefore(ctx, cutoff)
}

func (a *Adapter) ensureDriveFileSharer() *DriveFileSharer {
	a.previewMu.Lock()
	defer a.previewMu.Unlock()
	if a.client == nil {
		return nil
	}
	if a.fileSharer != nil {
		return a.fileSharer
	}
	a.fileSharer = NewDriveFileSharer(NewLarkDrivePreviewAPI(a.client), FileShareConfig{})
	return a.fileSharer
}

func (s *DriveFileSharer) ShareFile(ctx context.Context, req SharedFileRequest) (SharedFileResult, error) {
	if s == nil || s.api == nil {
		return SharedFileResult{}, fmt.Errorf("drive file sharer is not available")
	}
	principals := previewPrincipals(req.ChatID, req.UserID)
	if len(principals) == 0 {
		return SharedFileResult{}, fmt.Errorf("missing chat/user context for file sharing")
	}

	localPath, info, err := validateSharedLocalFile(req.LocalPath, s.config.MaxFileBytes)
	if err != nil {
		return SharedFileResult{}, err
	}
	content, err := os.ReadFile(localPath)
	if err != nil {
		return SharedFileResult{}, fmt.Errorf("read local file for sharing %s: %w", localPath, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	root, err := s.ensureRootFolderLocked(ctx)
	if err != nil {
		return SharedFileResult{}, err
	}
	token, err := s.api.UploadFile(ctx, root.Token, fileShareUploadName(localPath, time.Now().UTC()), content)
	if err != nil {
		return SharedFileResult{}, fmt.Errorf("upload shared file %s: %w", localPath, err)
	}
	url, err := s.api.QueryMetaURL(ctx, token, previewFileType)
	if err != nil {
		return SharedFileResult{}, fmt.Errorf("query shared file url for %s: %w", localPath, err)
	}
	if err := ensurePreviewPermissions(ctx, s.api, token, previewFileType, map[string]bool{}, principals); err != nil {
		return SharedFileResult{}, fmt.Errorf("authorize shared file %s: %w", localPath, err)
	}
	return SharedFileResult{
		FileName:  filepath.Base(localPath),
		URL:       strings.TrimSpace(url),
		SizeBytes: info.Size(),
	}, nil
}

func (s *DriveFileSharer) CleanupBefore(ctx context.Context, cutoff time.Time) (PreviewDriveCleanupResult, error) {
	if s == nil {
		return PreviewDriveCleanupResult{}, nil
	}
	if s.api == nil {
		return PreviewDriveCleanupResult{}, fmt.Errorf("drive file sharer is not available")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	root, err := s.ensureRootFolderLocked(ctx)
	if err != nil {
		return PreviewDriveCleanupResult{}, err
	}
	nodes, err := s.api.ListFiles(ctx, root.Token)
	if err != nil {
		return PreviewDriveCleanupResult{}, err
	}
	result := PreviewDriveCleanupResult{}
	for _, node := range nodes {
		if !strings.EqualFold(strings.TrimSpace(node.Type), previewFileType) {
			continue
		}
		createdAt, ok := fileShareManagedFileTime(node.Name)
		if !ok || !createdAt.Before(cutoff) {
			continue
		}
		if strings.TrimSpace(node.Token) != "" {
			if err := s.api.DeleteFile(ctx, node.Token, previewFileType); err != nil {
				return result, err
			}
		}
		result.DeletedFileCount++
	}
	return result, nil
}

func (s *DriveFileSharer) ensureRootFolderLocked(ctx context.Context) (*previewFolderRecord, error) {
	if s.root != nil && strings.TrimSpace(s.root.Token) != "" {
		return s.root, nil
	}
	nodes, err := s.api.ListFiles(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		if !strings.EqualFold(strings.TrimSpace(node.Type), previewFolderType) {
			continue
		}
		if strings.TrimSpace(node.Name) != strings.TrimSpace(s.config.RootFolderName) {
			continue
		}
		s.root = &previewFolderRecord{
			Token: strings.TrimSpace(node.Token),
			URL:   strings.TrimSpace(node.URL),
		}
		return s.root, nil
	}
	node, err := s.api.CreateFolder(ctx, s.config.RootFolderName, "")
	if err != nil {
		return nil, fmt.Errorf("create shared file root folder: %w", err)
	}
	s.root = &previewFolderRecord{
		Token: strings.TrimSpace(node.Token),
		URL:   strings.TrimSpace(node.URL),
	}
	return s.root, nil
}

func validateSharedLocalFile(localPath string, maxBytes int64) (string, os.FileInfo, error) {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return "", nil, fmt.Errorf("missing local file path")
	}
	if !filepath.IsAbs(localPath) {
		abs, err := filepath.Abs(localPath)
		if err != nil {
			return "", nil, err
		}
		localPath = abs
	}
	if real, err := filepath.EvalSymlinks(localPath); err == nil {
		localPath = real
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("path %q is not a regular file", localPath)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return "", nil, fmt.Errorf("file exceeds %d bytes: %s", maxBytes, localPath)
	}
	return filepath.Clean(localPath), info, nil
}

func fileShareUploadName(localPath string, now time.Time) string {
	base := strings.TrimSpace(filepath.Base(localPath))
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if name == "" {
		name = "download"
	}
	name = sanitizePreviewFileComponent(name)
	ext = sanitizeFileShareExt(ext)
	return fileShareManagedFilePrefix + now.UTC().Format(previewTimestampFormat) + "-" + name + ext
}

func fileShareManagedFileTime(name string) (time.Time, bool) {
	return managedFileTime(fileShareManagedFilePrefix, name)
}

func sanitizeFileShareExt(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range ext {
		switch {
		case r == '.':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	value := strings.TrimSpace(b.String())
	if value == "." {
		return ""
	}
	return value
}
