package feishu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultArtifactRootFolderName = "Feidex Artifacts"
	artifactFolderTimeFormat      = "20060102T150405.000000000Z"
)

type ArtifactUploadRequest struct {
	LocalPath string
	ChatID    string
	UserID    string
}

type ArtifactUploadResult struct {
	FolderToken string
	FolderName  string
	FileToken   string
	FileName    string
	URL         string
	SizeBytes   int64
	SHA256      string
	CreatedTime time.Time
}

type ArtifactStoreConfig struct {
	RootFolderName string
	MaxFileBytes   int64
}

type DriveArtifactStore struct {
	api    previewDriveAPI
	config ArtifactStoreConfig

	mu   sync.Mutex
	root *previewFolderRecord
}

func NewDriveArtifactStore(api previewDriveAPI, cfg ArtifactStoreConfig) *DriveArtifactStore {
	if cfg.RootFolderName == "" {
		cfg.RootFolderName = defaultArtifactRootFolderName
	}
	return &DriveArtifactStore{api: api, config: cfg}
}

func (s *DriveArtifactStore) UploadLocalFile(ctx context.Context, req ArtifactUploadRequest) (ArtifactUploadResult, error) {
	if s == nil || s.api == nil {
		return ArtifactUploadResult{}, fmt.Errorf("drive artifact store is not available")
	}
	principals := previewPrincipals(req.ChatID, req.UserID)
	if len(principals) == 0 {
		return ArtifactUploadResult{}, fmt.Errorf("missing chat/user context for artifact upload")
	}

	localPath, info, err := validateLocalArtifactFile(req.LocalPath, s.config.MaxFileBytes)
	if err != nil {
		return ArtifactUploadResult{}, err
	}
	sha256Hex, err := sha256File(localPath)
	if err != nil {
		return ArtifactUploadResult{}, err
	}
	createdAt := time.Now().UTC()
	folderName := artifactFolderName(createdAt, sha256Hex)
	fileName := filepath.Base(localPath)

	s.mu.Lock()
	defer s.mu.Unlock()

	root, err := s.ensureRootFolderLocked(ctx)
	if err != nil {
		return ArtifactUploadResult{}, err
	}
	folder, err := s.api.CreateFolder(ctx, folderName, root.Token)
	if err != nil {
		return ArtifactUploadResult{}, fmt.Errorf("create artifact folder %s: %w", folderName, err)
	}
	token, err := s.api.UploadFile(ctx, folder.Token, fileName, localPath)
	if err != nil {
		return ArtifactUploadResult{}, fmt.Errorf("upload artifact %s: %w", localPath, err)
	}
	url, err := s.api.QueryMetaURL(ctx, token, previewFileType)
	if err != nil {
		return ArtifactUploadResult{}, fmt.Errorf("query artifact url for %s: %w", localPath, err)
	}
	if err := ensurePreviewPermissions(ctx, s.api, token, previewFileType, map[string]bool{}, principals); err != nil {
		return ArtifactUploadResult{}, fmt.Errorf("authorize artifact %s: %w", localPath, err)
	}
	return ArtifactUploadResult{
		FolderToken: strings.TrimSpace(folder.Token),
		FolderName:  folderName,
		FileToken:   strings.TrimSpace(token),
		FileName:    fileName,
		URL:         strings.TrimSpace(url),
		SizeBytes:   info.Size(),
		SHA256:      sha256Hex,
		CreatedTime: createdAt,
	}, nil
}

func (s *DriveArtifactStore) CleanupBefore(ctx context.Context, cutoff time.Time) (PreviewDriveCleanupResult, error) {
	if s == nil {
		return PreviewDriveCleanupResult{}, nil
	}
	if s.api == nil {
		return PreviewDriveCleanupResult{}, fmt.Errorf("drive artifact store is not available")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	root, err := s.findRootFolderLocked(ctx)
	if err != nil || root == nil || strings.TrimSpace(root.Token) == "" {
		return PreviewDriveCleanupResult{}, err
	}
	nodes, err := s.api.ListFiles(ctx, root.Token)
	if err != nil {
		return PreviewDriveCleanupResult{}, err
	}
	result := PreviewDriveCleanupResult{}
	for _, node := range nodes {
		createdAt := node.CreatedTime
		if createdAt.IsZero() {
			var ok bool
			createdAt, ok = artifactFolderCreatedTime(node.Name)
			if !ok {
				continue
			}
		}
		if !createdAt.Before(cutoff) {
			continue
		}
		docType := strings.TrimSpace(node.Type)
		if docType == "" {
			docType = previewFolderType
		}
		if strings.TrimSpace(node.Token) == "" {
			continue
		}
		if err := s.api.DeleteFile(ctx, node.Token, docType); err != nil {
			return result, err
		}
		result.DeletedFileCount++
	}
	return result, nil
}

func (s *DriveArtifactStore) ensureRootFolderLocked(ctx context.Context) (*previewFolderRecord, error) {
	if s.root != nil && strings.TrimSpace(s.root.Token) != "" {
		return s.root, nil
	}
	roots, err := s.listRootFoldersLocked(ctx)
	if err != nil {
		return nil, err
	}
	if len(roots) > 0 {
		s.root = roots[0]
		return s.root, nil
	}
	node, err := s.api.CreateFolder(ctx, s.config.RootFolderName, "")
	if err != nil {
		return nil, fmt.Errorf("create artifact root folder: %w", err)
	}
	s.root = &previewFolderRecord{
		Token: strings.TrimSpace(node.Token),
		URL:   strings.TrimSpace(node.URL),
	}
	return s.root, nil
}

func (s *DriveArtifactStore) findRootFolderLocked(ctx context.Context) (*previewFolderRecord, error) {
	if s.root != nil && strings.TrimSpace(s.root.Token) != "" {
		return s.root, nil
	}
	roots, err := s.listRootFoldersLocked(ctx)
	if err != nil || len(roots) == 0 {
		return nil, err
	}
	s.root = roots[0]
	return s.root, nil
}

func (s *DriveArtifactStore) listRootFoldersLocked(ctx context.Context) ([]*previewFolderRecord, error) {
	if s == nil || s.api == nil {
		return nil, nil
	}
	nodes, err := s.api.ListFiles(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]*previewFolderRecord, 0, len(nodes))
	for _, node := range nodes {
		if !strings.EqualFold(strings.TrimSpace(node.Type), previewFolderType) {
			continue
		}
		if strings.TrimSpace(node.Name) != strings.TrimSpace(s.config.RootFolderName) {
			continue
		}
		out = append(out, &previewFolderRecord{
			Token: strings.TrimSpace(node.Token),
			URL:   strings.TrimSpace(node.URL),
		})
	}
	return out, nil
}

func sha256File(localPath string) (string, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("open file for hashing %s: %w", localPath, err)
	}
	defer f.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", fmt.Errorf("hash file %s: %w", localPath, err)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func validateLocalArtifactFile(localPath string, maxBytes int64) (string, os.FileInfo, error) {
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

func artifactFolderName(createdAt time.Time, sha256Hex string) string {
	createdAt = createdAt.UTC()
	sha256Hex = strings.TrimSpace(strings.ToLower(sha256Hex))
	if sha256Hex == "" {
		sha256Hex = "unknown"
	}
	return createdAt.Format(artifactFolderTimeFormat) + "-" + sha256Hex
}

func artifactFolderCreatedTime(name string) (time.Time, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return time.Time{}, false
	}
	idx := strings.IndexByte(name, '-')
	if idx <= 0 {
		return time.Time{}, false
	}
	parsed, err := time.Parse(artifactFolderTimeFormat, name[:idx])
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}
