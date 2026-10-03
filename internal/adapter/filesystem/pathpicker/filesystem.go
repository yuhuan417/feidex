// Package pathpicker implements local filesystem access for path selection.
package pathpicker

import (
	domain "feidex/internal/domain/workspace"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Filesystem struct{}

func (Filesystem) ResolvePath(root, candidate string) (string, error) {
	return ResolvePathPickerPath(root, candidate)
}
func (Filesystem) List(payload Payload) ([]Entry, int, int, error) {
	entries, total, hidden, _, err := ListPathPickerEntries(payload)
	return entries, total, hidden, err
}

type Payload = domain.PathPickerPayload
type Entry = domain.PathPickerEntry

const ModeDirectory = domain.PathPickerModeDirectory

func ResolvePathPickerRoot(ws *domain.Workspace) (string, error) {
	if ws == nil {
		return "", fmt.Errorf("current workspace not found")
	}
	root := strings.TrimSpace(ws.Cwd)
	if root == "" {
		return "", fmt.Errorf("workspace %q cwd is empty", ws.ID)
	}
	if !filepath.IsAbs(root) {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", err
		}
		root = abs
	}
	root = filepath.Clean(root)
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = filepath.Clean(real)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("workspace cwd %q is not accessible: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace cwd %q is not a directory", root)
	}
	return root, nil
}

func ResolvePathPickerPath(rootPath, candidate string) (string, error) {
	rootPath = filepath.Clean(strings.TrimSpace(rootPath))
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		candidate = rootPath
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(rootPath, candidate)
	}
	candidate = filepath.Clean(candidate)
	real, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	real = filepath.Clean(real)
	if !WithinRoot(rootPath, real) {
		return "", fmt.Errorf("path %q is outside workspace root", real)
	}
	return real, nil
}

func WithinRoot(rootPath, candidate string) bool {
	rootPath = filepath.Clean(strings.TrimSpace(rootPath))
	candidate = filepath.Clean(strings.TrimSpace(candidate))
	if rootPath == "" || candidate == "" {
		return false
	}
	if rootPath == string(filepath.Separator) {
		return filepath.IsAbs(candidate)
	}
	if candidate == rootPath {
		return true
	}
	return strings.HasPrefix(candidate, rootPath+string(filepath.Separator))
}

func ListPathPickerEntries(payload Payload) ([]Entry, int, int, int, error) {
	items, err := os.ReadDir(payload.CurrentPath)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	entries := make([]Entry, 0, len(items))
	hiddenFiles := 0
	for _, item := range items {
		name := strings.TrimSpace(item.Name())
		if name == "" {
			continue
		}
		entry := Entry{
			Name:  name,
			Path:  filepath.Join(payload.CurrentPath, name),
			IsDir: item.IsDir(),
		}
		if payload.Mode == ModeDirectory && !entry.IsDir {
			hiddenFiles++
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, len(entries), hiddenFiles, 0, nil
}
