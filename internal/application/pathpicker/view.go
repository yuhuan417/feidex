package pathpicker

import (
	"path/filepath"
	"strings"

	domain "feidex/internal/domain/workspace"
)

type Filesystem interface {
	ResolvePath(string, string) (string, error)
	List(domain.PathPickerPayload) ([]domain.PathPickerEntry, int, int, error)
}

type View struct {
	Payload     domain.PathPickerPayload
	Entries     []domain.PathPickerEntry
	Total       int
	HiddenFiles int
}

type Service struct{ Filesystem Filesystem }

func (s Service) Snapshot(payload domain.PathPickerPayload) (View, error) {
	if strings.TrimSpace(payload.Mode) != domain.PathPickerModeFile {
		payload.Mode = domain.PathPickerModeDirectory
	} else {
		payload.Mode = domain.PathPickerModeFile
	}
	payload.Style = domain.PathPickerStyleDropdown
	current, err := s.Filesystem.ResolvePath(payload.RootPath, payload.CurrentPath)
	if err != nil {
		return View{}, err
	}
	payload.CurrentPath = current
	if strings.TrimSpace(payload.SelectedPath) != "" {
		payload.SelectedPath, _ = s.Filesystem.ResolvePath(payload.RootPath, payload.SelectedPath)
	}
	entries, total, hidden, err := s.Filesystem.List(payload)
	if err != nil {
		return View{}, err
	}
	return View{Payload: payload, Entries: entries, Total: total, HiddenFiles: hidden}, nil
}

func (s Service) SelectedPathForConfirm(payload domain.PathPickerPayload) (string, error) {
	selected := payload.SelectedPath
	if payload.Mode == domain.PathPickerModeDirectory {
		selected = payload.CurrentPath
	}
	return s.Filesystem.ResolvePath(payload.RootPath, selected)
}

func ParentPath(payload domain.PathPickerPayload) string {
	if filepath.Clean(payload.CurrentPath) == filepath.Clean(payload.RootPath) {
		return payload.CurrentPath
	}
	return filepath.Dir(payload.CurrentPath)
}
