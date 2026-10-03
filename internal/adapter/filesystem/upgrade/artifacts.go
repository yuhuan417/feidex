package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	apppathpick "feidex/internal/adapter/filesystem/pathpicker"
	upgradeapp "feidex/internal/application/upgrade"
	domainworkspace "feidex/internal/domain/workspace"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type PathPickerPayload = domainworkspace.PathPickerPayload

type Artifacts struct{ DataDir func() string }

func (Artifacts) Picker(ws *domainworkspace.Workspace) (PathPickerPayload, error) {
	root, err := apppathpick.ResolvePathPickerRoot(ws)
	if err != nil {
		return PathPickerPayload{}, err
	}
	return PathPickerPayload{
		Mode:        domainworkspace.PathPickerModeFile,
		Style:       domainworkspace.PathPickerStyleDropdown,
		RootPath:    root,
		CurrentPath: root,
	}, nil
}

func (Artifacts) Resolve(ws *domainworkspace.Workspace, rawPath string) (string, error) {
	root, err := apppathpick.ResolvePathPickerRoot(ws)
	if err != nil {
		return "", err
	}
	resolved, err := apppathpick.ResolvePathPickerPath(root, rawPath)
	if err != nil {
		return "", fmt.Errorf("解析本地 Binary 路径失败: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("读取本地 Binary 失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("本地 Binary 必须是普通文件")
	}
	return resolved, nil
}

func (s Artifacts) Stage(ctx context.Context, requestID, sourcePath string) (upgradeapp.Artifact, error) {
	if err := ctx.Err(); err != nil {
		return upgradeapp.Artifact{}, err
	}
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return upgradeapp.Artifact{}, fmt.Errorf("missing local binary path")
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return upgradeapp.Artifact{}, fmt.Errorf("读取本地制品失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return upgradeapp.Artifact{}, fmt.Errorf("本地制品不是普通文件")
	}
	dir := filepath.Join(s.DataDir(), "upgrades", requestID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return upgradeapp.Artifact{}, err
	}
	targetPath := filepath.Join(dir, filepath.Base(sourcePath))
	if filepath.Clean(targetPath) == filepath.Clean(sourcePath) {
		targetPath = filepath.Join(dir, "artifact-"+filepath.Base(sourcePath))
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return upgradeapp.Artifact{}, err
	}
	defer source.Close()
	target, err := os.Create(targetPath)
	if err != nil {
		return upgradeapp.Artifact{}, err
	}
	defer target.Close()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(target, hash), source)
	if err != nil {
		return upgradeapp.Artifact{}, err
	}
	if err := target.Chmod(0o755); err != nil {
		return upgradeapp.Artifact{}, err
	}
	return upgradeapp.Artifact{Path: targetPath, Name: filepath.Base(sourcePath), SHA256: hex.EncodeToString(hash.Sum(nil)), Size: written}, nil
}
