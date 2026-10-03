package presentation

import (
	"feidex/internal/textutil"
	"strings"
)

func CurrentThreadLabel(name, preview, id string) string {
	for _, value := range []string{name, preview, id} {
		if strings.TrimSpace(value) != "" {
			return textutil.Truncate(value, 32)
		}
	}
	return "-"
}
