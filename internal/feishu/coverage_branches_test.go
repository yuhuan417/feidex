package feishu

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

func TestArtifactStoreBranches(t *testing.T) {
	store := NewDriveArtifactStore(nil, ArtifactStoreConfig{})
	if store == nil || store.config.RootFolderName != defaultArtifactRootFolderName {
		t.Fatalf("NewDriveArtifactStore() = %+v", store)
	}
	if _, err := store.UploadLocalFile(context.Background(), ArtifactUploadRequest{}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("UploadLocalFile(nil api) error = %v", err)
	}

	var nilStore *DriveArtifactStore
	if got, err := nilStore.CleanupBefore(context.Background(), time.Now()); err != nil || got != (PreviewDriveCleanupResult{}) {
		t.Fatalf("nil CleanupBefore() = %+v, %v", got, err)
	}
	store = &DriveArtifactStore{}
	if _, err := store.CleanupBefore(context.Background(), time.Now()); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("CleanupBefore(nil api) error = %v", err)
	}

	api := &fakePreviewAPI{}
	store = NewDriveArtifactStore(api, ArtifactStoreConfig{})
	if _, err := store.UploadLocalFile(context.Background(), ArtifactUploadRequest{LocalPath: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "missing chat/user context") {
		t.Fatalf("UploadLocalFile(missing principals) error = %v", err)
	}

	root, err := store.ensureRootFolderLocked(context.Background())
	if err != nil || root == nil || root.Token == "" {
		t.Fatalf("ensureRootFolderLocked(create) = %+v, %v", root, err)
	}
	if found, err := store.findRootFolderLocked(context.Background()); err != nil || found == nil || found.Token != root.Token {
		t.Fatalf("findRootFolderLocked(cached) = %+v, %v", found, err)
	}
	if roots, err := store.listRootFoldersLocked(context.Background()); err != nil || len(roots) != 1 {
		t.Fatalf("listRootFoldersLocked() = %+v, %v", roots, err)
	}

	api.root = &previewRemoteNode{Token: "folder-2", URL: "https://drive.example/folder-2", Type: previewFolderType, Name: defaultArtifactRootFolderName}
	store = NewDriveArtifactStore(api, ArtifactStoreConfig{})
	if found, err := store.findRootFolderLocked(context.Background()); err != nil || found == nil || found.Token != "folder-2" {
		t.Fatalf("findRootFolderLocked(list) = %+v, %v", found, err)
	}
}

func TestArtifactLocalFileValidationBranches(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(report.txt) error = %v", err)
	}
	relPath, err := filepath.Rel(".", filePath)
	if err != nil {
		relPath = filePath
	}
	validated, info, err := validateLocalArtifactFile(relPath, 10)
	if err != nil || !filepath.IsAbs(validated) || info == nil || info.Size() != 5 {
		t.Fatalf("validateLocalArtifactFile(relative) = %q, %+v, %v", validated, info, err)
	}
	if _, _, err := validateLocalArtifactFile(root, 10); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("validateLocalArtifactFile(dir) error = %v", err)
	}
	if _, _, err := validateLocalArtifactFile(filePath, 1); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("validateLocalArtifactFile(size) error = %v", err)
	}
	if _, err := sha256File(filepath.Join(root, "missing")); err == nil {
		t.Fatal("sha256File(missing) should fail")
	}
	if _, ok := artifactFolderCreatedTime("bad"); ok {
		t.Fatal("artifactFolderCreatedTime(bad) should fail")
	}
}

func TestPermissionIssueAndMessagePacerBranches(t *testing.T) {
	issue := &PermissionIssue{API: "drive.permission_member.create"}
	if got := (&driveAPIError{Issue: issue}).PermissionIssue(); got != issue {
		t.Fatalf("driveAPIError.PermissionIssue() = %+v, want issue", got)
	}

	if got := firstNonEmptyString("", " a ", "b"); got != " a " {
		t.Fatalf("firstNonEmptyString() = %q", got)
	}
	if got := flattenPermissionIssueDetails(&PermissionIssue{
		Details:         []PermissionIssueDetail{{Key: "scope", Value: "im:message"}},
		FieldViolations: []PermissionIssueFieldViolation{{Field: "chat_id", Value: "oc_1", Description: "required"}},
	}); !strings.Contains(got, "scope im:message") || !strings.Contains(got, "chat_id oc_1 required") {
		t.Fatalf("flattenPermissionIssueDetails() = %q", got)
	}

	if got := permissionIssueFromDirectError("im.message.create", errors.New("permission denied")); got == nil || got.API != "im.message.create" {
		t.Fatalf("permissionIssueFromDirectError(permission) = %+v", got)
	}
	if got := permissionIssueFromDirectError("im.message.create", errors.New("plain failure")); got != nil {
		t.Fatalf("permissionIssueFromDirectError(plain) = %+v, want nil", got)
	}

	direct := larkcore.CodeError{Code: 99991668, Msg: "forbidden"}
	if issue, ok := PermissionIssueFromError(direct); !ok || issue == nil || issue.Code != 99991668 {
		t.Fatalf("PermissionIssueFromError(code error) = %+v, %v", issue, ok)
	}
	if issue, ok := PermissionIssueFromError(errors.New("plain")); ok || issue != nil {
		t.Fatalf("PermissionIssueFromError(plain) = %+v, %v, want nil false", issue, ok)
	}

	if got := newRequestPacer(0); got == nil || got.interval != 0 {
		t.Fatalf("newRequestPacer(0) = %+v", got)
	}
	if delay, err := (*requestPacer)(nil).Wait(context.Background()); err != nil || delay != 0 {
		t.Fatalf("nil requestPacer Wait() = %v, %v", delay, err)
	}

	keyed := newKeyedRequestPacer(0)
	if keyed == nil || keyed.interval != 0 {
		t.Fatalf("newKeyedRequestPacer(0) = %+v", keyed)
	}
	if delay, err := keyed.Wait(context.Background(), ""); err != nil || delay != 0 {
		t.Fatalf("empty keyed Wait() = %v, %v", delay, err)
	}

	keyed = newKeyedRequestPacerWithInterval(time.Millisecond, time.Millisecond, time.Millisecond)
	keyed.entries["stale"] = &keyedRequestPacerEntry{pacer: newRequestPacerWithInterval(time.Millisecond), lastUsed: time.Now().Add(-time.Hour)}
	keyed.lastSweep = time.Now().Add(-time.Hour)
	_ = keyed.entryFor("fresh")
	if _, ok := keyed.entries["stale"]; ok {
		t.Fatalf("entryFor() should sweep stale entries: %+v", keyed.entries)
	}
	if !keyed.shouldSweep(time.Now().Add(time.Hour)) {
		t.Fatal("shouldSweep() should trigger after sweep interval")
	}
}
