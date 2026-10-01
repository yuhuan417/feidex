package feishu

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type errorPreviewAPI struct {
	fakePreviewAPI
	createFolderErr   error
	createFolderErrAt int
	listErr           error
	uploadErr         error
	queryErr          error
	grantErr          error
	deleteErr         error
}

type staticPreviewAPI struct {
	nodes map[string][]previewRemoteNode
}

func (f *errorPreviewAPI) CreateFolder(ctx context.Context, name, parentToken string) (previewRemoteNode, error) {
	if f.createFolderErr != nil && (f.createFolderErrAt <= 0 || f.fakePreviewAPI.createFolderCalls+1 == f.createFolderErrAt) {
		return previewRemoteNode{}, f.createFolderErr
	}
	return f.fakePreviewAPI.CreateFolder(ctx, name, parentToken)
}

func (f *errorPreviewAPI) ListFiles(ctx context.Context, folderToken string) ([]previewRemoteNode, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.fakePreviewAPI.ListFiles(ctx, folderToken)
}

func (f *errorPreviewAPI) UploadFile(ctx context.Context, parentToken, fileName, localPath string) (string, error) {
	if f.uploadErr != nil {
		return "", f.uploadErr
	}
	return f.fakePreviewAPI.UploadFile(ctx, parentToken, fileName, localPath)
}

func (f *errorPreviewAPI) QueryMetaURL(ctx context.Context, token, fileType string) (string, error) {
	if f.queryErr != nil {
		return "", f.queryErr
	}
	return f.fakePreviewAPI.QueryMetaURL(ctx, token, fileType)
}

func (f *errorPreviewAPI) GrantPermission(ctx context.Context, token, fileType string, principal previewPrincipal) error {
	if f.grantErr != nil {
		return f.grantErr
	}
	return f.fakePreviewAPI.GrantPermission(ctx, token, fileType, principal)
}

func (f *errorPreviewAPI) DeleteFile(ctx context.Context, token, fileType string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return f.fakePreviewAPI.DeleteFile(ctx, token, fileType)
}

func (f *staticPreviewAPI) CreateFolder(context.Context, string, string) (previewRemoteNode, error) {
	return previewRemoteNode{}, nil
}

func (f *staticPreviewAPI) ListFiles(_ context.Context, folderToken string) ([]previewRemoteNode, error) {
	return append([]previewRemoteNode(nil), f.nodes[strings.TrimSpace(folderToken)]...), nil
}

func (f *staticPreviewAPI) UploadFile(context.Context, string, string, string) (string, error) {
	return "", nil
}

func (f *staticPreviewAPI) QueryMetaURL(context.Context, string, string) (string, error) {
	return "", nil
}

func (f *staticPreviewAPI) GrantPermission(context.Context, string, string, previewPrincipal) error {
	return nil
}

func (f *staticPreviewAPI) DeleteFile(context.Context, string, string) error {
	return nil
}

func TestDriveArtifactStoreUploadAndAdapterBranches(t *testing.T) {
	root := t.TempDir()
	localPath := filepath.Join(root, "report.txt")
	if err := os.WriteFile(localPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(report.txt) error = %v", err)
	}

	api := &fakePreviewAPI{}
	store := NewDriveArtifactStore(api, ArtifactStoreConfig{})
	uploaded, err := store.UploadLocalFile(context.Background(), ArtifactUploadRequest{
		LocalPath: localPath,
		ChatID:    "oc_1",
		UserID:    "ou_1",
	})
	if err != nil {
		t.Fatalf("UploadLocalFile() error = %v", err)
	}
	if uploaded.FileName != "report.txt" || uploaded.FileToken == "" || uploaded.FolderToken == "" || uploaded.URL == "" || uploaded.SizeBytes != 5 || len(uploaded.SHA256) == 0 {
		t.Fatalf("UploadLocalFile() = %+v", uploaded)
	}

	adapter := &Adapter{
		client:        lark.NewClient("app", "secret"),
		artifactStore: store,
	}
	shared, err := adapter.ShareLocalFile(context.Background(), SharedFileRequest{
		LocalPath: localPath,
		ChatID:    "oc_2",
		UserID:    "ou_2",
	})
	if err != nil {
		t.Fatalf("ShareLocalFile() error = %v", err)
	}
	if shared.FileName != "report.txt" || shared.URL == "" || shared.SizeBytes != 5 {
		t.Fatalf("ShareLocalFile() = %+v", shared)
	}

	rootChildren := api.children["folder-1"]
	if len(rootChildren) == 0 {
		t.Fatalf("root children = %+v, want uploaded artifact folder", api.children)
	}
	rootChildren[0].CreatedTime = time.Time{}
	rootChildren[0].Name = artifactFolderName(time.Now().Add(-48*time.Hour), uploaded.SHA256)
	api.children["folder-1"] = rootChildren
	cleanup, err := adapter.CleanupArtifactsBefore(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("CleanupArtifactsBefore() error = %v", err)
	}
	if cleanup.DeletedFileCount != 1 {
		t.Fatalf("CleanupArtifactsBefore() = %+v, want 1 deleted artifact folder", cleanup)
	}
}

func TestDriveArtifactStoreErrorBranches(t *testing.T) {
	root := t.TempDir()
	localPath := filepath.Join(root, "report.txt")
	if err := os.WriteFile(localPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(report.txt) error = %v", err)
	}
	req := ArtifactUploadRequest{LocalPath: localPath, ChatID: "oc_1", UserID: "ou_1"}

	for _, tc := range []struct {
		name string
		api  *errorPreviewAPI
		want string
	}{
		{
			name: "root create failure",
			api: &errorPreviewAPI{
				createFolderErr:   errors.New("root boom"),
				createFolderErrAt: 1,
			},
			want: "create artifact root folder",
		},
		{
			name: "child create failure",
			api: &errorPreviewAPI{
				createFolderErr:   errors.New("child boom"),
				createFolderErrAt: 2,
			},
			want: "create artifact folder",
		},
		{
			name: "upload failure",
			api: &errorPreviewAPI{
				uploadErr: errors.New("upload boom"),
			},
			want: "upload artifact",
		},
		{
			name: "query failure",
			api: &errorPreviewAPI{
				queryErr: errors.New("query boom"),
			},
			want: "query artifact url",
		},
		{
			name: "grant failure",
			api: &errorPreviewAPI{
				grantErr: errors.New("grant boom"),
			},
			want: "authorize artifact",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewDriveArtifactStore(tc.api, ArtifactStoreConfig{})
			if _, err := store.UploadLocalFile(context.Background(), req); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("UploadLocalFile() error = %v, want substring %q", err, tc.want)
			}
		})
	}

	listAPI := &errorPreviewAPI{listErr: errors.New("list boom")}
	listAPI.root = &previewRemoteNode{Token: "folder-1", Type: previewFolderType, Name: defaultArtifactRootFolderName}
	store := NewDriveArtifactStore(listAPI, ArtifactStoreConfig{})
	if _, err := store.CleanupBefore(context.Background(), time.Now()); err == nil || !strings.Contains(err.Error(), "list boom") {
		t.Fatalf("CleanupBefore(list) error = %v", err)
	}

	deleteAPI := &errorPreviewAPI{deleteErr: errors.New("delete boom")}
	deleteAPI.root = &previewRemoteNode{Token: "folder-1", Type: previewFolderType, Name: defaultArtifactRootFolderName}
	deleteAPI.children = map[string][]previewRemoteNode{
		"folder-1": {
			{
				Token: "old-folder",
				Type:  previewFolderType,
				Name:  artifactFolderName(time.Now().Add(-48*time.Hour), "abc"),
			},
		},
	}
	store = NewDriveArtifactStore(deleteAPI, ArtifactStoreConfig{})
	if result, err := store.CleanupBefore(context.Background(), time.Now().Add(-24*time.Hour)); err == nil || result.DeletedFileCount != 0 || !strings.Contains(err.Error(), "delete boom") {
		t.Fatalf("CleanupBefore(delete) = %+v, %v", result, err)
	}

	filterAPI := &staticPreviewAPI{nodes: map[string][]previewRemoteNode{
		"": {
			{Token: "keep", Type: previewFolderType, Name: defaultArtifactRootFolderName, URL: "https://drive.example/keep"},
			{Token: "skip-type", Type: previewFileType, Name: defaultArtifactRootFolderName},
			{Token: "skip-name", Type: previewFolderType, Name: "Other"},
		},
	}}
	store = NewDriveArtifactStore(filterAPI, ArtifactStoreConfig{})
	if roots, err := store.listRootFoldersLocked(context.Background()); err != nil || len(roots) != 1 || roots[0].Token != "keep" {
		t.Fatalf("listRootFoldersLocked(filter) = %+v, %v", roots, err)
	}

	if got := artifactFolderName(time.Unix(0, 0), ""); !strings.Contains(got, "unknown") {
		t.Fatalf("artifactFolderName(empty sha) = %q", got)
	}
}
