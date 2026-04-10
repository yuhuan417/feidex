package feishu

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDriveFileSharerShareAndCleanup(t *testing.T) {
	root := t.TempDir()
	localPath := filepath.Join(root, "report.txt")
	if err := os.WriteFile(localPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(report.txt) error = %v", err)
	}
	api := &fakePreviewAPI{}
	sharer := NewDriveFileSharer(api, FileShareConfig{MaxFileBytes: 1024})

	result, err := sharer.ShareFile(context.Background(), SharedFileRequest{
		LocalPath: localPath,
		ChatID:    "oc_1",
		UserID:    "ou_1",
	})
	if err != nil {
		t.Fatalf("ShareFile() error = %v", err)
	}
	if result.FileName != "report.txt" || !strings.Contains(result.URL, "file-1") || result.SizeBytes != 5 {
		t.Fatalf("ShareFile() = %+v", result)
	}
	if api.createFolderCalls != 1 || api.uploadCalls != 1 || api.queryCalls != 1 {
		t.Fatalf("unexpected drive api usage: %+v", api)
	}
	if len(api.grantCalls) != 2 {
		t.Fatalf("grantCalls = %+v, want user + chat", api.grantCalls)
	}

	if len(api.files) != 1 {
		t.Fatalf("shared files = %+v, want 1", api.files)
	}
	api.files[0].Name = fileShareUploadName(localPath, time.Now().Add(-8*24*time.Hour))
	cleanup, err := sharer.CleanupBefore(context.Background(), time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("CleanupBefore() error = %v", err)
	}
	if cleanup.DeletedFileCount != 1 {
		t.Fatalf("CleanupBefore() = %+v, want 1 deleted file", cleanup)
	}
}

func TestFileShareHelpers(t *testing.T) {
	if got := fileShareUploadName("/tmp/a report!.txt", time.Unix(1700000000, 0)); !strings.HasPrefix(got, fileShareManagedFilePrefix) || !strings.HasSuffix(got, ".txt") {
		t.Fatalf("fileShareUploadName() = %q", got)
	}
	if ts, ok := fileShareManagedFileTime(fileShareManagedFilePrefix + time.Unix(1700000000, 0).UTC().Format(previewTimestampFormat) + "-report.txt"); !ok || ts.IsZero() {
		t.Fatalf("fileShareManagedFileTime() = %v, %v", ts, ok)
	}
	if got := sanitizeFileShareExt(".tar.gz"); got != ".tar.gz" {
		t.Fatalf("sanitizeFileShareExt() = %q", got)
	}
	if _, _, err := validateSharedLocalFile("", 1); err == nil {
		t.Fatal("expected missing local path to fail")
	}
}
