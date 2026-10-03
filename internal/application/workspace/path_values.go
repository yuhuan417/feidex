package workspace

import (
	"path/filepath"
	"strings"
	"unicode"

	domain "feidex/internal/domain/workspace"
)

// SuggestedIDFromDir derives a stable workspace ID from a selected directory.
func SuggestedIDFromDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(dir))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	return SuggestedID(base)
}

// SuggestedID normalizes a display name into a workspace ID.
func SuggestedID(raw string) string {
	var out strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			out.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if out.Len() > 0 && !lastDash {
				out.WriteByte('-')
				lastDash = true
			}
		default:
			if out.Len() > 0 && !lastDash {
				out.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(out.String(), "-")
}

// UpdateNewSuggestedID updates the automatically derived ID while preserving
// an explicit user-entered ID.
func UpdateNewSuggestedID(payload domain.NewPayload, selectedDir string) domain.NewPayload {
	nextAuto := SuggestedIDFromDir(selectedDir)
	currentDraft := strings.TrimSpace(payload.DraftID)
	currentAuto := strings.TrimSpace(payload.AutoDraftID)
	if nextAuto != "" && (currentDraft == "" || currentDraft == currentAuto) {
		payload.DraftID = nextAuto
	}
	payload.AutoDraftID = nextAuto
	return payload
}
