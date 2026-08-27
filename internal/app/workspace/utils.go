package workspace

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"feidex/internal/app/apputil"
	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

// SuggestedID normalizes raw into a lowercase, dash-separated workspace ID.
func SuggestedID(raw string) string {
	repoName := strings.ToLower(strings.TrimSpace(raw))
	var out strings.Builder
	lastDash := false
	for _, r := range repoName {
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

// SuggestedIDFromDir derives a workspace ID from a directory path.
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

// CloneDefaultID returns the default workspace ID for a cloned repo.
func CloneDefaultID(repoName string) string {
	return SuggestedID(repoName)
}

// CloneRepoName extracts the repository name from a git URL.
func CloneRepoName(repoURL string) (string, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", fmt.Errorf("git 地址不能为空")
	}
	pathPart := repoURL
	if !strings.Contains(repoURL, "://") {
		if idx := strings.Index(repoURL, ":"); idx > 0 && !strings.Contains(repoURL[:idx], "/") && strings.Contains(repoURL[idx+1:], "/") {
			pathPart = repoURL[idx+1:]
		}
	} else if parsed, err := url.Parse(repoURL); err == nil {
		pathPart = apputil.FirstNonEmpty(strings.TrimSpace(parsed.Path), strings.TrimSpace(parsed.Opaque))
	}
	base := strings.TrimSpace(strings.TrimSuffix(path.Base(strings.TrimSuffix(pathPart, "/")), ".git"))
	if base == "" || base == "." || base == "/" {
		return "", fmt.Errorf("无法从 git 地址推导仓库名")
	}
	return base, nil
}

// MergeNewFormValues merges form values into a NewPayload.
func MergeNewFormValues(payload NewPayload, values map[string]any) NewPayload {
	if value, ok := apputil.FormValueString(values, "workspace_id"); ok {
		if value != "" {
			payload.DraftID = value
			if strings.TrimSpace(payload.AutoDraftID) != value {
				payload.AutoDraftID = ""
			}
		} else if strings.TrimSpace(payload.DraftID) == strings.TrimSpace(payload.AutoDraftID) {
			payload.DraftID = ""
			payload.AutoDraftID = ""
		}
	}
	if value, ok := apputil.FormValueString(values, "workspace_name"); ok {
		payload.DraftName = value
	}
	return payload
}

// MergeCloneFormValues merges form values into a ClonePayload.
func MergeCloneFormValues(payload ClonePayload, values map[string]any) ClonePayload {
	if value, ok := apputil.FormValueString(values, "repo_url"); ok {
		payload.RepoURL = value
	}
	if value, ok := apputil.FormValueString(values, "workspace_id"); ok {
		payload.DraftID = value
	}
	if value, ok := apputil.FormValueString(values, "clone_mode"); ok {
		payload.CloneMode = NormalizeCloneMode(value)
	}
	if value, ok := apputil.FormValueString(values, "worktree_branch_name"); ok {
		payload.WorktreeBranchName = value
	}
	if value, ok := apputil.FormValueString(values, "worktree_workspace_id"); ok {
		payload.WorktreeWorkspaceID = value
	}
	if value, ok := apputil.FormValueString(values, "worktree_directory_name"); ok {
		payload.WorktreeDirectoryName = value
	}
	return payload
}

// NormalizeCloneMode canonicalizes the clone output mode.
func NormalizeCloneMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CloneModeWorktree:
		return CloneModeWorktree
	default:
		return CloneModeWorkspace
	}
}

// CloneCreatesWorktree reports whether the clone flow should create the final
// workspace from a git worktree rather than the cloned base repository itself.
func CloneCreatesWorktree(payload ClonePayload) bool {
	return NormalizeCloneMode(payload.CloneMode) == CloneModeWorktree
}

// WorktreePayloadFromPending extracts a WorktreePayload from a pending request.
func WorktreePayloadFromPending(pending *state.PendingRequest) WorktreePayload {
	var payload WorktreePayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}

// MergeWorktreeFormValues merges Feishu form values into a WorktreePayload.
func MergeWorktreeFormValues(payload WorktreePayload, values map[string]any) WorktreePayload {
	if value, ok := apputil.FormValueString(values, "base_workspace_id"); ok {
		payload.BaseWorkspaceID = value
	}
	if value, ok := apputil.FormValueString(values, "branch_name"); ok {
		payload.BranchName = value
	}
	if value, ok := apputil.FormValueString(values, "workspace_id"); ok {
		payload.WorkspaceID = value
	}
	if value, ok := apputil.FormValueString(values, "directory_name"); ok {
		payload.DirectoryName = value
	}
	return payload
}

// ParseWorktreeArgs parses /workspace new worktree arguments. The base
// workspace remains the current selected workspace; optional arguments only
// prefill the branch and workspace ID.
func ParseWorktreeArgs(args []string) (branchName, workspaceID string, err error) {
	if len(args) < 2 || strings.TrimSpace(args[0]) != "new" || strings.TrimSpace(args[1]) != "worktree" {
		return "", "", fmt.Errorf("usage: %s", CommandUsage)
	}
	switch len(args) {
	case 2:
		return "", "", nil
	case 3:
		return strings.TrimSpace(args[2]), "", nil
	case 4:
		return strings.TrimSpace(args[2]), strings.TrimSpace(args[3]), nil
	default:
		return "", "", fmt.Errorf("usage: %s", CommandUsage)
	}
}

// SuggestedWorktreeID returns a human-readable default workspace ID for a
// worktree based on the base project and bot name.
func SuggestedWorktreeID(baseProject, botName string) string {
	base := SuggestedID(baseProject)
	if base == "" {
		base = "workspace"
	}
	bot := SuggestedID(botName)
	if bot == "" {
		bot = "bot"
	}
	return base + "-" + bot
}

// SuggestedWorktreeBranch returns a human-readable default branch name for a
// worktree based on the base project and bot name.
func SuggestedWorktreeBranch(baseProject, botName, workspaceID string) string {
	project := SuggestedID(baseProject)
	if project == "" {
		project = "workspace"
	}
	bot := SuggestedID(botName)
	if bot == "" {
		bot = "bot"
	}
	workspaceID = SuggestedID(workspaceID)
	parts := []string{"work", project, bot}
	if workspaceID != "" {
		parts = append(parts, workspaceID)
	}
	return strings.Join(parts, "/")
}

// NewTakeoverNotice returns the notice text for workspace takeover.
func NewTakeoverNotice(targetDir string) string {
	targetDir = apputil.FirstNonEmpty(strings.TrimSpace(targetDir), "-")
	return "clone 目标目录已存在，可直接新建工作区接管。\n\n目录已预填为 `" + targetDir + "`，并已带上建议的 `workspace_id`。"
}

// NewExistingWorkspaceNotice returns the notice for existing workspace.
func NewExistingWorkspaceNotice() string {
	return "该 workspace_id 已存在，并且目录与现有工作区一致。"
}

// UpdateNewSuggestedID updates the suggested ID in a NewPayload based on selected directory.
func UpdateNewSuggestedID(payload NewPayload, selectedDir string) NewPayload {
	nextAuto := SuggestedIDFromDir(selectedDir)
	currentDraft := strings.TrimSpace(payload.DraftID)
	currentAuto := strings.TrimSpace(payload.AutoDraftID)
	if nextAuto != "" && (currentDraft == "" || currentDraft == currentAuto) {
		payload.DraftID = nextAuto
	}
	payload.AutoDraftID = nextAuto
	return payload
}

// NewTakeoverPayload builds a NewPayload for workspace takeover.
func NewTakeoverPayload(workspaceID, targetDir string) NewPayload {
	return NewTakeoverPayloadWithNotice(workspaceID, targetDir, NewTakeoverNotice(targetDir))
}

// NewTakeoverPayloadWithNotice builds a NewPayload for workspace takeover with custom notice.
func NewTakeoverPayloadWithNotice(workspaceID, targetDir, notice string) NewPayload {
	targetDir = strings.TrimSpace(targetDir)
	suggestedID := apputil.FirstNonEmpty(strings.TrimSpace(workspaceID), SuggestedIDFromDir(targetDir))
	return NewPayload{
		RootPath:    "/",
		SelectedCWD: targetDir,
		DraftID:     suggestedID,
		AutoDraftID: suggestedID,
		Notice:      strings.TrimSpace(notice),
	}
}

// SessionReferencesWorkspace checks if a session references a given workspace ID.
func SessionReferencesWorkspace(sess *state.Session, workspaceID string) bool {
	if sess == nil {
		return false
	}
	workspaceID = strings.TrimSpace(workspaceID)
	return strings.TrimSpace(sess.WorkspaceID) == workspaceID || strings.TrimSpace(sess.ActiveThreadWorkspaceID) == workspaceID
}

// SortThreadsByUpdated sorts thread list entries by updated time (descending).
func SortThreadsByUpdated(items []codexrpc.ThreadListEntry) {
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
}
