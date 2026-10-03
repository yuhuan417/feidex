package backend

import "strings"

func DropCodexLineageAfterFailure(recovering bool, failure string) bool {
	if recovering {
		return true
	}
	failure = strings.ToLower(failure)
	for _, terminal := range []string{"codex client not initialized", "codex app-server read failed", "codex app-server stdin write failed", "codex app-server process exited"} {
		if strings.Contains(failure, terminal) {
			return true
		}
	}
	return false
}
