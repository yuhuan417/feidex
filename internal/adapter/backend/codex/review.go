package codex

import (
	"feidex/internal/domain/review"
	"strings"
)

// ReviewTargetParams converts a TargetSpec to a map for RPC parameters.
func ReviewTargetParams(target review.TargetSpec) map[string]any {
	switch strings.TrimSpace(target.Type) {
	case review.TargetUncommitted:
		return map[string]any{"type": review.TargetUncommitted}
	case review.TargetBaseBranch:
		return map[string]any{"type": review.TargetBaseBranch, "branch": strings.TrimSpace(target.Branch)}
	case review.TargetCommit:
		params := map[string]any{"type": review.TargetCommit, "sha": strings.TrimSpace(target.CommitSHA)}
		if title := strings.TrimSpace(target.CommitTitle); title != "" {
			params["title"] = title
		}
		return params
	case review.TargetCustom:
		return map[string]any{"type": review.TargetCustom, "instructions": strings.TrimSpace(target.Instructions)}
	default:
		return map[string]any{"type": review.TargetUncommitted}
	}
}
