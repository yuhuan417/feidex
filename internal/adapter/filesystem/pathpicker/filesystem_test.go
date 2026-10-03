package pathpicker

import (
	"os"
	"path/filepath"
	"testing"

	domain "feidex/internal/domain/workspace"
)

func TestResolvePathPickerPathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "file.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "inside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := ResolvePathPickerPath(root, filepath.Join("escape", "file.txt")); err == nil {
		t.Fatal("symlink escape accepted")
	}
	if path, err := ResolvePathPickerPath(root, "inside"); err != nil || path != filepath.Join(root, "inside") {
		t.Fatalf("inside path = %q, %v", path, err)
	}
}

func TestListPathPickerEntriesFiltersFilesInDirectoryMode(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "visible.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, total, hidden, _, err := ListPathPickerEntries(domain.PathPickerPayload{CurrentPath: root, Mode: domain.PathPickerModeDirectory})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || hidden != 1 || len(entries) != 1 || entries[0].Name != "dir" {
		t.Fatalf("entries=%+v total=%d hidden=%d", entries, total, hidden)
	}
}
