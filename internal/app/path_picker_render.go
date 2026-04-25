package app

import (
	apppathpick "feidex/internal/app/pathpick"
	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/config"
)

const (
	pathPickerKind          = appworkspace.PathPickerKind
	pathPickerModeDirectory = appworkspace.PathPickerModeDirectory
	pathPickerModeFile      = appworkspace.PathPickerModeFile
	pathPickerStyleDropdown = appworkspace.PathPickerStyleDropdown
)

type pathPickerPayload = appworkspace.PathPickerPayload
type pathPickerEntry = appworkspace.PathPickerEntry

func normalizePathPickerMode(mode string) string {
	return apppathpick.NormalizePathPickerMode(mode)
}

func normalizePathPickerStyle(style string) string {
	return apppathpick.NormalizePathPickerStyle(style)
}

func resolvePathPickerRoot(ws *config.Workspace) (string, error) {
	return apppathpick.ResolvePathPickerRoot(ws)
}

func resolvePathPickerPath(rootPath, candidate string) (string, error) {
	return apppathpick.ResolvePathPickerPath(rootPath, candidate)
}

func pathPickerWithinRoot(rootPath, candidate string) bool {
	return apppathpick.WithinRoot(rootPath, candidate)
}

// renderPathPickerCard is defined in workspace_creation_render.go via the
// workspaceRenderService wrapper.

func buildPathPickerDropdownElement(requestID string, payload pathPickerPayload, entries []pathPickerEntry) map[string]any {
	return apppathpick.BuildDropdownElement(requestID, payload, entries)
}

func buildPathPickerFooterElement(requestID string, payload pathPickerPayload) map[string]any {
	return apppathpick.BuildFooterElement(requestID, payload)
}

func listPathPickerEntries(payload pathPickerPayload) ([]pathPickerEntry, int, int, int, error) {
	return apppathpick.ListPathPickerEntries(payload)
}

func renderPathPickerEntryLabel(entry pathPickerEntry) string {
	return apppathpick.RenderEntryLabel(entry)
}

func encodePathPickerOption(entry pathPickerEntry) string {
	return apppathpick.EncodeOption(entry)
}

func decodePathPickerOption(raw string) (path string, isDir bool, ok bool) {
	return apppathpick.DecodeOption(raw)
}
