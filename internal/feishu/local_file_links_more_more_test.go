package feishu

import (
	"context"
	"testing"
)

func TestLocalFileLinkAdditionalHelpers(t *testing.T) {
	if got := (*driveAPIError)(nil).Error(); got != "" {
		t.Fatalf("nil driveAPIError Error() = %q", got)
	}
	if _, ok := previewUserPrincipal("on_123"); !ok {
		t.Fatal("previewUserPrincipal(unionid) should succeed")
	}
	if _, ok := previewChatPrincipal(""); ok {
		t.Fatal("previewChatPrincipal(empty) should fail")
	}
	if _, ok := previewManagedFileTime("bad"); ok {
		t.Fatal("previewManagedFileTime(bad) should fail")
	}

	api := &fakePreviewAPI{}
	p := NewDriveLocalFileLinkRewriter(api, LocalFileLinkConfig{})
	p.store.root = &previewFolderRecord{Token: "folder-1", URL: "https://drive.example/folder-1"}
	root, err := p.ensureRootFolderLocked(context.Background())
	if err != nil || root.Token != "folder-1" {
		t.Fatalf("ensureRootFolderLocked(existing) = %+v, %v", root, err)
	}
	if got, err := p.listRootFoldersLocked(context.Background()); err != nil || got == nil {
		t.Fatalf("listRootFoldersLocked() = %+v, %v", got, err)
	}
	api.root = &previewRemoteNode{Token: "folder-2", URL: "https://drive.example/folder-2", Type: previewFolderType, Name: defaultPreviewRootFolderName}
	p = NewDriveLocalFileLinkRewriter(api, LocalFileLinkConfig{})
	root, err = p.ensureRootFolderLocked(context.Background())
	if err != nil || root.Token != "folder-2" {
		t.Fatalf("ensureRootFolderLocked(list existing) = %+v, %v", root, err)
	}
	shared := map[string]bool{"openid:ou_1": true}
	if err := ensurePreviewPermissions(context.Background(), api, "token-1", previewFileType, shared, []previewPrincipal{{Key: "openid:ou_1", MemberType: "openid", MemberID: "ou_1", Type: "user"}}); err != nil {
		t.Fatalf("ensurePreviewPermissions(shared) error = %v", err)
	}
	if previewPathWithinAnyRoot("/tmp/outside", []string{"/tmp/root"}) {
		t.Fatal("previewPathWithinAnyRoot() should reject outside path")
	}
	if previewPathWithinRoot("/tmp/root", ".") {
		t.Fatal("previewPathWithinRoot(dot root) should reject")
	}
	if got := sanitizePreviewFileComponent("!!!"); got != "preview" {
		t.Fatalf("sanitizePreviewFileComponent(empty) = %q, want preview", got)
	}
}
