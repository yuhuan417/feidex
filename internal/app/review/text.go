package review

import (
	"strings"

	"feidex/internal/app/apputil"
)

func SubmissionInputText(target TargetSpec) string {
	switch strings.TrimSpace(target.Type) {
	case TargetUncommitted:
		return "Review: uncommitted changes"
	case TargetBaseBranch:
		return "Review: base branch " + strings.TrimSpace(target.Branch)
	case TargetCommit:
		label := ShortCommitSHA(target.CommitSHA)
		if strings.TrimSpace(target.CommitTitle) != "" {
			label += " " + strings.TrimSpace(target.CommitTitle)
		}
		return "Review: commit " + strings.TrimSpace(label)
	case TargetCustom:
		return "Review: " + apputil.Truncate(strings.TrimSpace(target.Instructions), 80)
	default:
		return "Review"
	}
}

func ConfirmationText(target TargetSpec) string {
	return "已启动 review，目标：" + TargetSummary(target) + "。"
}

func TargetSummary(target TargetSpec) string {
	switch strings.TrimSpace(target.Type) {
	case TargetUncommitted:
		return "未提交改动"
	case TargetBaseBranch:
		return "base branch `" + inlineCodeText(target.Branch) + "`"
	case TargetCommit:
		if title := strings.TrimSpace(target.CommitTitle); title != "" {
			return "commit `" + inlineCodeText(ShortCommitSHA(target.CommitSHA)) + "` " + title
		}
		return "commit `" + inlineCodeText(ShortCommitSHA(target.CommitSHA)) + "`"
	case TargetCustom:
		return "自定义 instructions"
	default:
		return "review"
	}
}

func ShortCommitSHA(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func inlineCodeText(s string) string { return apputil.InlineCodeText(s) }
