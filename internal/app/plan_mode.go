package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/app/modelconfig"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const planCommandUsage = "/plan | /plan on | /plan off"

func commandPlan(a *App, msg *feishu.InboundMessage, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: %s", planCommandUsage)
	}
	if len(args) == 1 {
		switch strings.TrimSpace(args[0]) {
		case "on", "off":
		default:
			return fmt.Errorf("usage: %s", planCommandUsage)
		}
	}
	if a == nil || msg == nil {
		return nil
	}
	sessionKey := makeSessionKey(a, msg)
	sess := a.State().Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return fmt.Errorf("当前没有活动线程，无法配置 plan mode")
	}
	currentMode := normalizeThreadCollaborationMode(sess.ActiveThreadCollaborationMode)
	switch {
	case len(args) == 0 && currentMode != nil && strings.EqualFold(currentMode.Mode, "plan"):
		defaultMode, err := resolveDefaultCodexCollaborationModeForSession(a, sess)
		if err != nil {
			return err
		}
		sess.ActiveThreadCollaborationMode = defaultMode
		if err := a.State().SaveSession(sess); err != nil {
			return err
		}
		invalidateCodexPlanModeExitArtifactsForSession(a, sessionKey, "当前 thread 已关闭 plan mode，旧的计划确认已失效。")
		return a.feishu.ReplyText(context.Background(), msg.MessageID, "当前 thread 已关闭 `plan` collaboration mode。", replyInThreadEnabled(a, msg.ChatType))
	case len(args) == 0:
		mode, err := resolvePlanModeForActiveThread(a)
		if err != nil {
			return err
		}
		sess.ActiveThreadCollaborationMode = mode
		if err := a.State().SaveSession(sess); err != nil {
			return err
		}
		invalidateCodexPlanModeExitArtifactsForSession(a, sessionKey, "当前 thread 已重新配置 plan mode，旧的计划确认已失效。")
		return a.feishu.ReplyText(context.Background(), msg.MessageID, renderPlanModeStatusText(mode), replyInThreadEnabled(a, msg.ChatType))
	case strings.TrimSpace(args[0]) == "off":
		defaultMode, err := resolveDefaultCodexCollaborationModeForSession(a, sess)
		if err != nil {
			return err
		}
		sess.ActiveThreadCollaborationMode = defaultMode
		if err := a.State().SaveSession(sess); err != nil {
			return err
		}
		invalidateCodexPlanModeExitArtifactsForSession(a, sessionKey, "当前 thread 已关闭 plan mode，旧的计划确认已失效。")
		return a.feishu.ReplyText(context.Background(), msg.MessageID, "当前 thread 已关闭 `plan` collaboration mode。", replyInThreadEnabled(a, msg.ChatType))
	default:
		mode, err := resolvePlanModeForActiveThread(a)
		if err != nil {
			return err
		}
		sess.ActiveThreadCollaborationMode = mode
		if err := a.State().SaveSession(sess); err != nil {
			return err
		}
		invalidateCodexPlanModeExitArtifactsForSession(a, sessionKey, "当前 thread 已重新配置 plan mode，旧的计划确认已失效。")
		return a.feishu.ReplyText(context.Background(), msg.MessageID, renderPlanModeStatusText(mode), replyInThreadEnabled(a, msg.ChatType))
	}
}

func renderPlanModeStatusText(mode *state.SessionCollaborationMode) string {
	mode = normalizeThreadCollaborationMode(mode)
	if mode == nil {
		return "当前 thread 未开启 `plan` collaboration mode。"
	}
	lines := []string{
		"当前 thread 已开启 `plan` collaboration mode。",
		"mode: `" + mode.Mode + "`",
		"model: `" + mode.Model + "`",
	}
	if effort := strings.TrimSpace(mode.ReasoningEffort); effort != "" {
		lines = append(lines, "reasoning_effort: `"+effort+"`")
	}
	lines = append(lines, "developer_instructions: `null`")
	return strings.Join(lines, "\n")
}

func resolvePlanModeForActiveThread(a *App) (*state.SessionCollaborationMode, error) {
	if a == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	if !a.cfg.Codex.ExperimentalAPI {
		return nil, fmt.Errorf("当前 Codex runtime 未启用 experimental API，`/plan` 不可用")
	}
	client, err := requireCodexClient(a)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var listResp codexrpc.CollaborationModeListResponse
	if err := client.Call(ctx, "collaborationMode/list", map[string]any{}, &listResp); err != nil {
		return nil, fmt.Errorf("读取 collaboration mode 列表失败: %w", err)
	}
	preset, err := modelconfig.FindPlanCollaborationModePreset(listResp)
	if err != nil {
		return nil, err
	}
	model, effort, err := resolvePlanModeSettings(ctx, a, client, preset)
	if err != nil {
		return nil, err
	}
	mode := &state.SessionCollaborationMode{
		Mode:  "plan",
		Model: model,
	}
	if strings.TrimSpace(effort) != "" {
		mode.ReasoningEffort = strings.TrimSpace(effort)
	}
	return normalizeThreadCollaborationMode(mode), nil
}

func planModeForSession(a *App, sessionKey string) *state.SessionCollaborationMode {
	if a == nil || strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	sess := a.State().Session(sessionKey)
	if sess == nil {
		return nil
	}
	return normalizeThreadCollaborationMode(sess.ActiveThreadCollaborationMode)
}

func sessionActiveThreadIDForLog(sess *state.Session) string {
	if sess == nil {
		return ""
	}
	return strings.TrimSpace(sess.ActiveThreadID)
}

func sessionActiveCollaborationModeForLog(sess *state.Session) *state.SessionCollaborationMode {
	if sess == nil {
		return nil
	}
	return sess.ActiveThreadCollaborationMode
}

func sessionBackendCollaborationModeForLog(sess *state.Session, backend string) *state.SessionCollaborationMode {
	if sess == nil || len(sess.BackendThreads) == 0 {
		return nil
	}
	snapshot, ok := sess.BackendThreads[strings.TrimSpace(backend)]
	if !ok {
		return nil
	}
	return snapshot.CollaborationMode
}

func resolveDefaultCodexCollaborationModeForSession(a *App, sess *state.Session) (*state.SessionCollaborationMode, error) {
	if mode := defaultCodexCollaborationModeForSession(a, sess); mode != nil {
		return mode, nil
	}
	if a == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	client, err := requireCodexClient(a)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	model, effort, err := resolveDefaultCollaborationModeSettings(ctx, a, client)
	if err != nil {
		return nil, fmt.Errorf("无法解析 default collaboration mode model: %w", err)
	}
	mode := &state.SessionCollaborationMode{
		Mode:  "default",
		Model: model,
	}
	if strings.TrimSpace(effort) != "" {
		mode.ReasoningEffort = strings.TrimSpace(effort)
	}
	return normalizeThreadCollaborationMode(mode), nil
}

func defaultCodexCollaborationModeForSession(a *App, sess *state.Session) *state.SessionCollaborationMode {
	model := ""
	effort := ""
	if a != nil && a.cfg != nil {
		model = strings.TrimSpace(configuredGlobalModel(a.cfg))
		effort = strings.TrimSpace(modelconfig.ConfiguredGlobalReasoningEffort(a.cfg))
	}
	if mode := normalizeThreadCollaborationMode(sessionActiveCollaborationModeForLog(sess)); mode != nil && strings.EqualFold(mode.Mode, "default") {
		if model == "" {
			model = strings.TrimSpace(mode.Model)
		}
		if effort == "" {
			effort = strings.TrimSpace(mode.ReasoningEffort)
		}
	}
	if mode := normalizeThreadCollaborationMode(sessionBackendCollaborationModeForLog(sess, backendCodex)); mode != nil && strings.EqualFold(mode.Mode, "default") {
		if model == "" {
			model = strings.TrimSpace(mode.Model)
		}
		if effort == "" {
			effort = strings.TrimSpace(mode.ReasoningEffort)
		}
	}
	if mode := normalizeThreadCollaborationMode(sessionActiveCollaborationModeForLog(sess)); mode != nil && canReuseCollaborationModeModelForDefault(a, mode) {
		if model == "" {
			model = strings.TrimSpace(mode.Model)
		}
	}
	if model == "" {
		if mode := normalizeThreadCollaborationMode(sessionBackendCollaborationModeForLog(sess, backendCodex)); mode != nil && canReuseCollaborationModeModelForDefault(a, mode) {
			model = strings.TrimSpace(mode.Model)
		}
	}
	if model == "" {
		slog.Debug("plan mode disable could not build default collaboration mode",
			"backend", configuredBackend(a),
			"active_thread_id", sessionActiveThreadIDForLog(sess),
		)
		return nil
	}
	mode := &state.SessionCollaborationMode{
		Mode:  "default",
		Model: model,
	}
	if strings.TrimSpace(effort) != "" {
		mode.ReasoningEffort = strings.TrimSpace(effort)
	}
	return normalizeThreadCollaborationMode(mode)
}

func canReuseCollaborationModeModelForDefault(a *App, mode *state.SessionCollaborationMode) bool {
	mode = normalizeThreadCollaborationMode(mode)
	if mode == nil {
		return false
	}
	if !strings.EqualFold(mode.Mode, "plan") {
		return true
	}
	if a == nil || a.cfg == nil {
		return true
	}
	return strings.TrimSpace(modelconfig.ConfiguredPlanModel(a.cfg)) == ""
}

func (a *App) PlanModeTitleForSession(sessionKey, title string) string {
	return planModeTitleForSession(a, sessionKey, title)
}

func planModeTitleForSession(a *App, sessionKey, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	title = sessionWorkspaceTitleForSession(a, sessionKey, title)
	mode := planModeForSession(a, sessionKey)
	if mode == nil || !strings.EqualFold(mode.Mode, "plan") {
		return title
	}
	return prependTitlePrefix(title, "[plan]")
}

func (a *App) ContentCardTitleForSession(sessionKey, workspaceID, title string) string {
	return contentCardTitleForSession(a, sessionKey, workspaceID, title)
}

func contentCardTitleForSubmission(a *App, sub *state.Submission, title string) string {
	if sub == nil {
		return strings.TrimSpace(title)
	}
	return contentCardTitleForSession(a, sub.SessionKey, sub.WorkspaceID, title)
}

func contentCardTitleForSession(a *App, sessionKey, workspaceID, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if strings.TrimSpace(workspaceID) == "" && a != nil && sessionKey != "" {
		if sess := a.State().Session(sessionKey); sess != nil {
			workspaceID = strings.TrimSpace(sess.WorkspaceID)
		}
	}
	planMode := false
	if mode := planModeForSession(a, sessionKey); mode != nil {
		planMode = strings.EqualFold(mode.Mode, "plan")
	}
	return normalizeContentCardTitle(title, workspaceID, planMode)
}

func normalizeContentCardTitle(title, workspaceID string, planMode bool) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	prefixes, rest := splitLeadingTitlePrefixes(title)
	desired := make([]string, 0, 2+len(prefixes))
	if ws := strings.TrimSpace(workspaceID); ws != "" {
		desired = append(desired, "["+ws+"]")
	}
	if planMode {
		desired = append(desired, "[plan]")
	}
	for _, prefix := range prefixes {
		if titlePrefixAlreadyPresent(desired, prefix) {
			continue
		}
		desired = append(desired, prefix)
	}
	if len(desired) == 0 {
		return rest
	}
	if rest == "" {
		return strings.Join(desired, " ")
	}
	return strings.Join(desired, " ") + " " + rest
}

func titlePrefixAlreadyPresent(prefixes []string, candidate string) bool {
	for _, existing := range prefixes {
		if strings.EqualFold(strings.TrimSpace(existing), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func sessionWorkspaceTitleForSession(a *App, sessionKey, title string) string {
	title = strings.TrimSpace(title)
	if title == "" || a == nil || strings.TrimSpace(sessionKey) == "" {
		return title
	}
	sess := a.State().Session(sessionKey)
	if sess == nil {
		return title
	}
	ws := strings.TrimSpace(sess.WorkspaceID)
	if ws == "" {
		return title
	}
	return prependTitlePrefix(title, "["+ws+"]")
}

func prependTitlePrefix(title, prefix string) string {
	title = strings.TrimSpace(title)
	prefix = strings.TrimSpace(prefix)
	if title == "" {
		return ""
	}
	if prefix == "" {
		return title
	}
	leadingPrefixes, rest := splitLeadingTitlePrefixes(title)
	for _, existing := range leadingPrefixes {
		if strings.EqualFold(existing, prefix) {
			return title
		}
	}
	parts := append(append([]string(nil), leadingPrefixes...), prefix)
	if rest == "" {
		return strings.Join(parts, " ")
	}
	return strings.Join(parts, " ") + " " + rest
}

func splitLeadingTitlePrefixes(title string) (prefixes []string, rest string) {
	rest = strings.TrimSpace(title)
	for {
		if !strings.HasPrefix(rest, "[") {
			break
		}
		end := strings.Index(rest, "] ")
		if end < 0 {
			if strings.HasSuffix(rest, "]") {
				prefixes = append(prefixes, rest)
				rest = ""
			}
			break
		}
		prefixes = append(prefixes, rest[:end+1])
		rest = strings.TrimSpace(rest[end+2:])
		if rest == "" {
			break
		}
	}
	return prefixes, rest
}

func resolvePlanModeSettings(ctx context.Context, a *App, client CodexClient, preset *codexrpc.CollaborationModeMask) (model string, effort string, err error) {
	model = strings.TrimSpace(modelconfig.ConfiguredPlanModel(a.cfg))
	if model == "" {
		model = strings.TrimSpace(configuredGlobalModel(a.cfg))
	}
	effort = strings.TrimSpace(modelconfig.ConfiguredPlanReasoningEffort(a.cfg))
	if effort == "" && preset != nil && preset.ReasoningEffort != nil {
		effort = strings.TrimSpace(*preset.ReasoningEffort)
	}
	if model != "" {
		return model, effort, nil
	}
	var result codexrpc.ModelListResult
	if err := client.Call(ctx, "model/list", map[string]any{
		"limit":         20,
		"includeHidden": false,
	}, &result); err != nil {
		return "", "", fmt.Errorf("读取 model 列表失败: %w", err)
	}
	entry, resolvedEffort := modelconfig.EffectivePlanConfiguredModelAndEffort(a.cfg, result, preset)
	if entry == nil {
		return "", "", fmt.Errorf("当前 Codex model 不可用，无法开启 `/plan`")
	}
	model = firstNonEmpty(strings.TrimSpace(entry.ID), strings.TrimSpace(entry.Model))
	if model == "" {
		return "", "", fmt.Errorf("当前 Codex model 不可用，无法开启 `/plan`")
	}
	return model, resolvedEffort, nil
}

func resolveDefaultCollaborationModeSettings(ctx context.Context, a *App, client CodexClient) (model string, effort string, err error) {
	model = strings.TrimSpace(configuredGlobalModel(a.cfg))
	effort = strings.TrimSpace(modelconfig.ConfiguredGlobalReasoningEffort(a.cfg))
	if model != "" {
		return model, effort, nil
	}
	var result codexrpc.ModelListResult
	if err := client.Call(ctx, "model/list", map[string]any{
		"limit":         20,
		"includeHidden": false,
	}, &result); err != nil {
		return "", "", fmt.Errorf("读取 model 列表失败: %w", err)
	}
	entry, resolvedEffort := modelconfig.EffectiveConfiguredModelAndEffort(a.cfg, result)
	if entry == nil {
		return "", "", fmt.Errorf("当前 Codex model 不可用，无法恢复 default collaboration mode")
	}
	model = firstNonEmpty(strings.TrimSpace(entry.ID), strings.TrimSpace(entry.Model))
	if model == "" {
		return "", "", fmt.Errorf("当前 Codex model 不可用，无法恢复 default collaboration mode")
	}
	if effort != "" {
		effort = strings.TrimSpace(resolvedEffort)
	}
	return model, effort, nil
}

func codexCollaborationModeForTurnStart(a *App, sessionKey, threadID string) *codexrpc.CollaborationMode {
	if a == nil || strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(threadID) == "" {
		return nil
	}
	sess := a.State().Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return nil
	}
	mode := defaultCollaborationModeWithConfiguredEffort(a, sess.ActiveThreadCollaborationMode)
	return codexCollaborationModeFromState(mode)
}

func codexCollaborationModeFromState(mode *state.SessionCollaborationMode) *codexrpc.CollaborationMode {
	mode = normalizeThreadCollaborationMode(mode)
	if mode == nil {
		return nil
	}
	var reasoningEffort *string
	if value := strings.TrimSpace(mode.ReasoningEffort); value != "" {
		reasoningEffort = &value
	}
	return &codexrpc.CollaborationMode{
		Mode: mode.Mode,
		Settings: codexrpc.CollaborationModeSettings{
			DeveloperInstructions: nil,
			Model:                 mode.Model,
			ReasoningEffort:       reasoningEffort,
		},
	}
}

func defaultCollaborationModeWithConfiguredEffort(a *App, mode *state.SessionCollaborationMode) *state.SessionCollaborationMode {
	mode = normalizeThreadCollaborationMode(mode)
	if mode == nil || !strings.EqualFold(mode.Mode, "default") || strings.TrimSpace(mode.ReasoningEffort) != "" {
		return mode
	}
	if a == nil || a.cfg == nil {
		return mode
	}
	effort := strings.TrimSpace(modelconfig.ConfiguredGlobalReasoningEffort(a.cfg))
	if effort == "" {
		return mode
	}
	cp := *mode
	cp.ReasoningEffort = effort
	return normalizeThreadCollaborationMode(&cp)
}

func normalizeThreadCollaborationMode(mode *state.SessionCollaborationMode) *state.SessionCollaborationMode {
	if mode == nil {
		return nil
	}
	cp := *mode
	cp.Mode = strings.TrimSpace(cp.Mode)
	cp.Model = strings.TrimSpace(cp.Model)
	cp.ReasoningEffort = strings.TrimSpace(cp.ReasoningEffort)
	if cp.Mode == "" || cp.Model == "" {
		return nil
	}
	return &cp
}

func derefStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
