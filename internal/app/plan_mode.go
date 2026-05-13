package app

import (
	"context"
	"fmt"
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
	switch {
	case len(args) == 0:
		return a.feishu.ReplyText(context.Background(), msg.MessageID, renderPlanModeStatusText(sess.ActiveThreadCollaborationMode), replyInThreadEnabled(a, msg.ChatType))
	case strings.TrimSpace(args[0]) == "off":
		sess.ActiveThreadCollaborationMode = nil
		if err := a.State().SaveSession(sess); err != nil {
			return err
		}
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
	preset, err := findPlanCollaborationModePreset(listResp)
	if err != nil {
		return nil, err
	}
	model, err := resolvePlanModeModel(ctx, a, client, preset)
	if err != nil {
		return nil, err
	}
	mode := &state.SessionCollaborationMode{
		Mode:  "plan",
		Model: model,
	}
	if preset.ReasoningEffort != nil {
		mode.ReasoningEffort = strings.TrimSpace(*preset.ReasoningEffort)
	}
	return normalizeThreadCollaborationMode(mode), nil
}

func findPlanCollaborationModePreset(resp codexrpc.CollaborationModeListResponse) (*codexrpc.CollaborationModeMask, error) {
	for i := range resp.Data {
		mode := strings.TrimSpace(derefStringPtr(resp.Data[i].Mode))
		if mode == "plan" {
			return &resp.Data[i], nil
		}
	}
	return nil, fmt.Errorf("当前 Codex app-server 未提供 `plan` collaboration mode")
}

func resolvePlanModeModel(ctx context.Context, a *App, client CodexClient, preset *codexrpc.CollaborationModeMask) (string, error) {
	if preset != nil {
		if model := strings.TrimSpace(derefStringPtr(preset.Model)); model != "" {
			return model, nil
		}
	}
	if model := strings.TrimSpace(configuredGlobalModel(a.cfg)); model != "" {
		return model, nil
	}
	var result codexrpc.ModelListResult
	if err := client.Call(ctx, "model/list", map[string]any{
		"limit":         20,
		"includeHidden": false,
	}, &result); err != nil {
		return "", fmt.Errorf("读取 model 列表失败: %w", err)
	}
	entry := modelconfig.DefaultModelEntry(result)
	if entry == nil {
		return "", fmt.Errorf("当前 Codex model 不可用，无法开启 `/plan`")
	}
	model := firstNonEmpty(strings.TrimSpace(entry.ID), strings.TrimSpace(entry.Model))
	if model == "" {
		return "", fmt.Errorf("当前 Codex model 不可用，无法开启 `/plan`")
	}
	return model, nil
}

func codexCollaborationModeForTurnStart(a *App, sessionKey, threadID string) *codexrpc.CollaborationMode {
	if a == nil || strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(threadID) == "" {
		return nil
	}
	sess := a.State().Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return nil
	}
	return codexCollaborationModeFromState(sess.ActiveThreadCollaborationMode)
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
