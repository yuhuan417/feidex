package conversation

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
)

// ThreadEntry describes a backend conversation without exposing its wire protocol.
type ThreadEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Preview   string `json:"preview"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	Source    string `json:"source"`
	Cwd       string `json:"cwd"`
}

type ThreadBinding struct {
	ThreadID string
	Name     string
	Preview  string
	Resumed  bool
}

type ThreadSelection struct {
	ThreadID string
	Name     string
	Preview  string
	Cwd      string
}

type WarningError struct{ Message string }

func (e WarningError) Error() string  { return e.Message }
func NewWarning(message string) error { return WarningError{Message: strings.TrimSpace(message)} }
func IsWarning(err error) bool        { var target WarningError; return errors.As(err, &target) }

func SameWorkspaceCWD(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func SortThreadsByUpdated(items []ThreadEntry) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].UpdatedAt != items[j].UpdatedAt {
			return items[i].UpdatedAt > items[j].UpdatedAt
		}
		return items[i].CreatedAt > items[j].CreatedAt
	})
}
