// Package clauderuntime provides pure helper functions and types for the
// Claude runtime subsystem. These functions have no dependency on *App.
package clauderuntime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	appapproval "feidex/internal/app/approval"
	apputil "feidex/internal/app/apputil"
	appdelivery "feidex/internal/app/delivery"
	apppendingforms "feidex/internal/app/pendingforms"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/claudecli"
	"feidex/internal/codexrpc"
)

// CopyPermissionUpdates deep-copies a slice of permission update maps.
func CopyPermissionUpdates(in []map[string]any) []map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		copied := make(map[string]any, len(item))
		for key, value := range item {
			copied[key] = value
		}
		out = append(out, copied)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// TurnUsageAsThreadUsage converts a Claude TurnUsage to a Codex ThreadTokenUsage.
func TurnUsageAsThreadUsage(usage claudecli.TurnUsage) codexrpc.ThreadTokenUsage {
	last := codexrpc.TokenUsageBreakdown{
		InputTokens:       int64(usage.InputTokens),
		CachedInputTokens: int64(usage.CacheReadTokens + usage.CacheCreationTokens),
		OutputTokens:      int64(usage.OutputTokens),
	}
	last.TotalTokens = last.InputTokens + last.CachedInputTokens + last.OutputTokens
	total := last
	if usage.HasCumulativeUsage {
		total = codexrpc.TokenUsageBreakdown{
			InputTokens:       int64(usage.CumulativeInputTokens),
			CachedInputTokens: int64(usage.CumulativeCacheReadTokens + usage.CumulativeCacheCreationTokens),
			OutputTokens:      int64(usage.CumulativeOutputTokens),
		}
		total.TotalTokens = total.InputTokens + total.CachedInputTokens + total.OutputTokens
	}
	threadUsage := codexrpc.ThreadTokenUsage{
		Total: total,
		Last:  last,
	}
	if usage.ContextWindow > 0 {
		contextWindow := int64(usage.ContextWindow)
		threadUsage.ModelContextWindow = &contextWindow
	}
	return threadUsage
}

// TurnContextUsagePercent calculates the context window usage percentage.
func TurnContextUsagePercent(usage claudecli.TurnUsage) (float64, bool) {
	if usage.ContextWindow <= 0 {
		return 0, false
	}
	usedTokens := usage.InputTokens + usage.CacheCreationTokens + usage.CacheReadTokens
	if usedTokens < 0 {
		usedTokens = 0
	}
	percentage := float64(usedTokens) * 100 / float64(usage.ContextWindow)
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}
	return percentage, true
}

// QuestionsAsToolUserInput converts Claude questions to tool user input format.
func QuestionsAsToolUserInput(questions []claudecli.Question) []apppendingforms.ToolUserInputQuestion {
	out := make([]apppendingforms.ToolUserInputQuestion, 0, len(questions))
	for i, question := range questions {
		id := fmt.Sprintf("q%d", i+1)
		opts := make([]apppendingforms.ToolUserInputOption, 0, len(question.Options))
		for _, opt := range question.Options {
			opts = append(opts, apppendingforms.ToolUserInputOption{Label: strings.TrimSpace(opt.Label)})
		}
		out = append(out, apppendingforms.ToolUserInputQuestion{
			Header:      id,
			ID:          id,
			Question:    strings.TrimSpace(question.Text),
			Options:     opts,
			MultiSelect: question.MultiSelect,
		})
	}
	return out
}

// PlanModeBody builds the body text for a Claude plan mode card.
func PlanModeBody(plan claudecli.PlanInfo) string {
	lines := []string{
		"Claude 已完成计划阶段，请点击按钮或直接回复下一条消息作为反馈。",
		"",
		"可回复示例：",
		"- `请先改成 ...`",
		"- `方案里漏了 ...`",
	}
	if strings.TrimSpace(plan.Plan) != "" {
		lines = append(lines, "", "计划：", strings.TrimSpace(plan.Plan))
	}
	if len(plan.AllowedPrompts) > 0 {
		lines = append(lines, "", "计划申请的能力：")
		for _, prompt := range plan.AllowedPrompts {
			lines = append(lines, fmt.Sprintf("- `%s`: %s", strings.TrimSpace(prompt.Tool), strings.TrimSpace(prompt.Prompt)))
		}
	}
	return strings.Join(lines, "\n")
}

// IsInternalTool returns true if the tool name is a Claude internal tool.
func IsInternalTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "AskUserQuestion", "ExitPlanMode":
		return true
	default:
		return false
	}
}

// IsPlanFilePath returns true if path looks like a Claude plan file.
func IsPlanFilePath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	return path != "" && strings.Contains(path, ".claude/plans/") && strings.HasSuffix(strings.ToLower(path), ".md")
}

// PlanFilePathFromTool extracts a plan file path from a Write tool's input.
func PlanFilePathFromTool(toolName string, input map[string]interface{}) string {
	if strings.TrimSpace(toolName) != "Write" {
		return ""
	}
	path := strings.TrimSpace(apputil.FirstNonEmpty(
		apputil.StringValue(input["file_path"]),
		apputil.StringValue(input["path"]),
	))
	if !IsPlanFilePath(path) {
		return ""
	}
	return path
}

// EnrichPlanForDisplay enriches a PlanInfo with file content if the plan text is empty.
func EnrichPlanForDisplay(plan claudecli.PlanInfo, trackedPlanPath, workspaceCwd string, startedAt time.Time) claudecli.PlanInfo {
	if strings.TrimSpace(plan.Plan) != "" {
		return plan
	}
	if text := ReadPlanText(trackedPlanPath, workspaceCwd, startedAt); text != "" {
		plan.Plan = text
	}
	return plan
}

// ReadPlanText reads plan text from candidate files.
func ReadPlanText(trackedPlanPath, workspaceCwd string, startedAt time.Time) string {
	for _, candidate := range PlanFileCandidates(trackedPlanPath, workspaceCwd, startedAt) {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		if text := strings.TrimSpace(string(data)); text != "" {
			return text
		}
	}
	return ""
}

// PlanFileCandidates returns candidate plan file paths to check.
func PlanFileCandidates(trackedPlanPath, workspaceCwd string, startedAt time.Time) []string {
	seen := map[string]bool{}
	candidates := make([]string, 0, 4)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if !filepath.IsAbs(path) && strings.TrimSpace(workspaceCwd) != "" {
			path = filepath.Join(workspaceCwd, path)
		}
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		seen[path] = true
		candidates = append(candidates, path)
	}

	add(trackedPlanPath)
	if latest := LatestPlanFile(filepath.Join(strings.TrimSpace(workspaceCwd), ".claude", "plans"), startedAt); latest != "" {
		add(latest)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if latest := LatestPlanFile(filepath.Join(home, ".claude", "plans"), startedAt); latest != "" {
			add(latest)
		}
	}
	return candidates
}

// LatestPlanFile returns the most recently modified plan file in dir.
func LatestPlanFile(dir string, startedAt time.Time) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var latestPath string
	var latestTime time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		fullPath := filepath.Join(dir, entry.Name())
		if !IsPlanFilePath(fullPath) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		modTime := info.ModTime()
		if !startedAt.IsZero() && modTime.Before(startedAt.Add(-1*time.Minute)) {
			continue
		}
		if latestPath == "" || modTime.After(latestTime) {
			latestPath = fullPath
			latestTime = modTime
		}
	}
	return latestPath
}

// IsFatalSessionError returns true if the event represents a fatal session error.
func IsFatalSessionError(sessionStopped bool, sessionExitError bool, eventError error) bool {
	if eventError == nil {
		return false
	}
	var procErr *claudecli.ProcessError
	if errors.As(eventError, &procErr) {
		return true
	}
	if sessionStopped {
		return true
	}
	return sessionExitError
}

// ClaudeTurnState represents the state of a Claude turn (pure data).
type ClaudeTurnState struct {
	TurnNumber int
	TurnID     string
	Thinking   string

	LastAssistantText        string
	LastTextChunks           []appdelivery.SentReplyChunk
	DeliveredAnyText         bool
	SuppressFailedCompletion bool
}

// ClaudePendingResponse represents a pending response from a Claude interaction.
type ClaudePendingResponse struct {
	Approval *claudecli.PermissionResponse
	Answers  map[string]string
	Feedback string
	Err      error
}

// InteractiveHandler handles interactive Claude events (questions, plan mode).
type InteractiveHandler struct {
	AskQuestion func(interface{}, []claudecli.Question) (map[string]string, error)
	ExitPlan    func(interface{}, claudecli.PlanInfo) (string, error)
}

// HandleAskUserQuestion delegates to the ask question handler.
func (h *InteractiveHandler) HandleAskUserQuestion(ctx interface{}, questions []claudecli.Question) (map[string]string, error) {
	if h == nil || h.AskQuestion == nil {
		return nil, fmt.Errorf("interactive handler not configured")
	}
	return h.AskQuestion(ctx, questions)
}

// HandleExitPlanMode delegates to the exit plan handler.
func (h *InteractiveHandler) HandleExitPlanMode(ctx interface{}, plan claudecli.PlanInfo) (string, error) {
	if h == nil || h.ExitPlan == nil {
		return "", fmt.Errorf("interactive handler not configured")
	}
	return h.ExitPlan(ctx, plan)
}

// RenderApprovalPresentation computes the approval card kind, body, and payload
// for a Claude permission request. workspaceCwdFunc is called to resolve workspace cwd.
func RenderApprovalPresentation(workspaceID string, req *claudecli.PermissionRequest, workspaceCwdFunc func(string) string) appapproval.Presentation {
	request := map[string]any{
		"tool":       req.ToolName,
		"toolName":   req.ToolName,
		"tool_input": req.Input,
	}
	if req.BlockedPath != nil && strings.TrimSpace(*req.BlockedPath) != "" {
		request["blockedPath"] = strings.TrimSpace(*req.BlockedPath)
	}
	presentation := appapproval.Presentation{
		Payload: appapproval.RequestPayload{
			Request: request,
		},
	}
	switch strings.TrimSpace(req.ToolName) {
	case "Bash", "KillShell":
		presentation.Kind = appapproval.KindCommand
		presentation.Payload.Request["command"] = strings.TrimSpace(apputil.FirstNonEmpty(apputil.StringValue(req.Input["command"]), apputil.StringValue(req.Input["cmd"])))
		presentation.Body = appapproval.RenderCommandBody(presentation.Payload.Request)
	case "Write", "Edit", "NotebookEdit":
		presentation.Kind = appapproval.KindFile
		path := strings.TrimSpace(apputil.FirstNonEmpty(
			apputil.StringValue(req.Input["file_path"]),
			apputil.StringValue(req.Input["path"]),
			apputil.StringValue(req.Input["notebook_path"]),
		))
		if path == "" && req.BlockedPath != nil {
			path = strings.TrimSpace(*req.BlockedPath)
		}
		presentation.Payload.Request["changes"] = []map[string]any{{"path": path, "kind": strings.TrimSpace(req.ToolName)}}
		cwd := ""
		if workspaceCwdFunc != nil {
			cwd = workspaceCwdFunc(workspaceID)
		}
		presentation.Body = appapproval.RenderFileBodyWithWorkspace(presentation.Payload.Request, cwd)
	default:
		presentation.Kind = appapproval.KindPermissions
		presentation.Payload.Permissions = map[string]any{
			"tool": req.ToolName,
			"blocked_path": apputil.FirstNonEmpty(func() string {
				if req.BlockedPath == nil {
					return ""
				}
				return strings.TrimSpace(*req.BlockedPath)
			}(), ""),
		}
		presentation.Payload.Request["permissions"] = appapproval.CloneJSONMap(presentation.Payload.Permissions)
		presentation.Body = appapproval.RenderPermissionsApprovalBody(presentation.Payload.Request)
	}
	presentation.Payload.Body = presentation.Body
	return presentation
}

// NormalizePermissionMode normalizes a Claude permission mode string.
func NormalizePermissionMode(value string) string {
	switch strings.TrimSpace(value) {
	case "", "default":
		return string(appruntime.ClaudePermissionModeDefault)
	case string(appruntime.ClaudePermissionModeAcceptEdits):
		return string(appruntime.ClaudePermissionModeAcceptEdits)
	case string(appruntime.ClaudePermissionModeBypass):
		return string(appruntime.ClaudePermissionModeBypass)
	case string(appruntime.ClaudePermissionModePlan):
		return string(appruntime.ClaudePermissionModePlan)
	default:
		return strings.TrimSpace(value)
	}
}

// PermissionModeValue converts a permission mode string to claudecli.PermissionMode.
func PermissionModeValue(mode string) claudecli.PermissionMode {
	switch NormalizePermissionMode(mode) {
	case string(appruntime.ClaudePermissionModeAcceptEdits):
		return claudecli.PermissionModeAcceptEdits
	case string(appruntime.ClaudePermissionModePlan):
		return claudecli.PermissionModePlan
	case string(appruntime.ClaudePermissionModeBypass):
		return claudecli.PermissionModeBypass
	default:
		return claudecli.PermissionModeDefault
	}
}

// SafeClaudeSessionPermissionUpdates filters and normalises permission
// update suggestions, returning only valid session-scoped updates.
func SafeClaudeSessionPermissionUpdates(suggestions []map[string]any) []SessionPermissionUpdate {
	if len(suggestions) == 0 {
		return nil
	}
	updates := make([]SessionPermissionUpdate, 0, len(suggestions))
	for _, suggestion := range suggestions {
		normalized, ok := NormalizeSessionPermissionUpdate(suggestion)
		if ok {
			updates = append(updates, normalized)
		}
	}
	if len(updates) == 0 {
		return nil
	}
	return updates
}

// DescribeClaudeSessionPermissionUpdates returns a human-readable summary
// of a list of session permission updates.
func DescribeClaudeSessionPermissionUpdates(updates []SessionPermissionUpdate) string {
	if len(updates) == 0 {
		return ""
	}
	if len(updates) == 1 {
		update := updates[0]
		switch update.Type {
		case SessionPermissionUpdateTypeSetMode:
			mode := NormalizePermissionMode(update.Mode)
			if mode != "" {
				return "切到 `" + mode + "`（当前会话）"
			}
		case SessionPermissionUpdateTypeAddRules:
			return "添加权限规则（当前会话）"
		}
	}
	return fmt.Sprintf("更新 %d 项权限（当前会话）", len(updates))
}
