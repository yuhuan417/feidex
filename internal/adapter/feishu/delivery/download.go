package delivery

import (
	"fmt"
	"path/filepath"
	"strings"
)

func RenderDownloadDisplayPath(selectedPath, workspaceCWD string) string {
	selectedPath = strings.TrimSpace(selectedPath)
	workspaceCWD = strings.TrimSpace(workspaceCWD)
	if selectedPath == "" {
		return "-"
	}
	if workspaceCWD != "" {
		if rel, err := filepath.Rel(workspaceCWD, selectedPath); err == nil && strings.TrimSpace(rel) != "" && !strings.HasPrefix(rel, "..") {
			return filepath.Clean(rel)
		}
	}
	return selectedPath
}

func FormatDownloadSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(size)
	unit := "B"
	for _, next := range units {
		value /= 1024
		unit = next
		if value < 1024 {
			break
		}
	}
	if unit == "KB" {
		return fmt.Sprintf("%.1f %s", value, unit)
	}
	return fmt.Sprintf("%.2f %s", value, unit)
}
