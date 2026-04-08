package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func configuredGlobalModel(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Codex.Model)
}

func configuredGlobalReasoningEffort(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Codex.ReasoningEffort)
}

func defaultModelEntry(result codexrpc.ModelListResult) *codexrpc.ModelListEntry {
	for i := range result.Data {
		if result.Data[i].IsDefault {
			return &result.Data[i]
		}
	}
	if len(result.Data) == 0 {
		return nil
	}
	return &result.Data[0]
}

func findModelEntry(result codexrpc.ModelListResult, modelID string) *codexrpc.ModelListEntry {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return defaultModelEntry(result)
	}
	for i := range result.Data {
		if result.Data[i].ID == modelID || result.Data[i].Model == modelID {
			return &result.Data[i]
		}
	}
	return defaultModelEntry(result)
}

func modelSupportsEffort(model *codexrpc.ModelListEntry, effort string) bool {
	effort = strings.TrimSpace(effort)
	if model == nil || effort == "" {
		return true
	}
	for _, item := range model.SupportedReasoningEfforts {
		if strings.TrimSpace(item.ReasoningEffort) == effort {
			return true
		}
	}
	return false
}

func effectiveConfiguredModelAndEffort(cfg *config.Config, result codexrpc.ModelListResult) (model *codexrpc.ModelListEntry, effort string) {
	model = findModelEntry(result, configuredGlobalModel(cfg))
	effort = configuredGlobalReasoningEffort(cfg)
	if effort == "" && model != nil {
		effort = strings.TrimSpace(model.DefaultReasoningEffort)
	}
	if !modelSupportsEffort(model, effort) && model != nil {
		effort = strings.TrimSpace(model.DefaultReasoningEffort)
	}
	return model, effort
}

func (a *App) fetchModelList(ctx context.Context) (codexrpc.ModelListResult, error) {
	var result codexrpc.ModelListResult
	if a.codex == nil {
		return result, fmt.Errorf("codex client not initialized")
	}
	if err := a.codex.Call(ctx, "model/list", map[string]any{"limit": 100, "includeHidden": false}, &result); err != nil {
		return result, err
	}
	return result, nil
}

func modelCardActionRow(buttons []feishu.Button) map[string]any {
	actions := make([]map[string]any, 0, len(buttons))
	for _, btn := range buttons {
		actions = append(actions, map[string]any{
			"tag":   "button",
			"type":  btn.Type,
			"text":  map[string]any{"tag": "plain_text", "content": btn.Text},
			"value": btn.Value,
		})
	}
	return map[string]any{"tag": "action", "actions": actions}
}

func chunkButtons(buttons []feishu.Button, size int) [][]feishu.Button {
	if len(buttons) == 0 {
		return nil
	}
	if size <= 0 {
		size = len(buttons)
	}
	rows := make([][]feishu.Button, 0, (len(buttons)+size-1)/size)
	for len(buttons) > 0 {
		n := size
		if len(buttons) < n {
			n = len(buttons)
		}
		rows = append(rows, append([]feishu.Button(nil), buttons[:n]...))
		buttons = buttons[n:]
	}
	return rows
}

func (a *App) renderModelConfigCard(result codexrpc.ModelListResult, sessionKey string) map[string]any {
	selectedModel, selectedEffort := effectiveConfiguredModelAndEffort(a.cfg, result)
	modelName := "(default)"
	modelDescription := ""
	if selectedModel != nil {
		modelName = firstNonEmpty(selectedModel.DisplayName, selectedModel.ID, selectedModel.Model)
		modelDescription = strings.TrimSpace(selectedModel.Description)
	}
	modelValue := configuredGlobalModel(a.cfg)
	effortValue := configuredGlobalReasoningEffort(a.cfg)
	modelSource := "跟随 app-server 默认"
	if modelValue != "" {
		modelSource = "全局显式配置"
	}
	effortSource := "跟随模型默认"
	if effortValue != "" {
		effortSource = "全局显式配置"
	}

	elements := []map[string]any{
		{
			"tag": "markdown",
			"content": "当前模型: `" + modelName + "`\n" +
				"模型来源: " + modelSource + "\n" +
				"当前推理强度: `" + firstNonEmpty(selectedEffort, "-") + "`\n" +
				"推理来源: " + effortSource +
				func() string {
					if modelDescription == "" {
						return ""
					}
					return "\n\n" + modelDescription
				}(),
		},
		{"tag": "markdown", "content": "选择全局模型"},
	}

	modelButtons := []feishu.Button{{
		Text: func() string {
			if modelValue == "" {
				return "当前 · 跟随默认"
			}
			return "跟随默认"
		}(),
		Type: func() string {
			if modelValue == "" {
				return "primary"
			}
			return "default"
		}(),
		Value: map[string]any{"action": "model.config.set_model", "model_id": ""},
	}}
	if strings.TrimSpace(sessionKey) != "" {
		modelButtons[0].Value["session_key"] = sessionKey
	}
	for _, item := range result.Data {
		label := firstNonEmpty(item.DisplayName, item.ID, item.Model)
		btnType := "default"
		if selectedModel != nil && item.ID == selectedModel.ID && modelValue != "" {
			label = "当前 · " + label
			btnType = "primary"
		}
		modelButtons = append(modelButtons, feishu.Button{
			Text:  label,
			Type:  btnType,
			Value: map[string]any{"action": "model.config.set_model", "model_id": item.ID, "session_key": sessionKey},
		})
	}
	for _, row := range chunkButtons(modelButtons, 3) {
		elements = append(elements, modelCardActionRow(row))
	}

	elements = append(elements, map[string]any{"tag": "markdown", "content": "选择全局推理强度"})
	effortButtons := []feishu.Button{{
		Text: func() string {
			if effortValue == "" {
				return "当前 · 跟随默认"
			}
			return "跟随默认"
		}(),
		Type: func() string {
			if effortValue == "" {
				return "primary"
			}
			return "default"
		}(),
		Value: map[string]any{"action": "model.config.set_effort", "reasoning_effort": ""},
	}}
	if strings.TrimSpace(sessionKey) != "" {
		effortButtons[0].Value["session_key"] = sessionKey
	}
	if selectedModel != nil {
		for _, item := range selectedModel.SupportedReasoningEfforts {
			label := item.ReasoningEffort
			btnType := "default"
			if item.ReasoningEffort == selectedEffort && effortValue != "" {
				label = "当前 · " + label
				btnType = "primary"
			}
			effortButtons = append(effortButtons, feishu.Button{
				Text:  label,
				Type:  btnType,
				Value: map[string]any{"action": "model.config.set_effort", "reasoning_effort": item.ReasoningEffort, "session_key": sessionKey},
			})
		}
	}
	for _, row := range chunkButtons(effortButtons, 3) {
		elements = append(elements, modelCardActionRow(row))
	}
	if strings.TrimSpace(sessionKey) != "" {
		elements = append(elements, modelCardActionRow([]feishu.Button{{
			Text:  "返回菜单",
			Type:  "default",
			Value: map[string]any{"action": "menu.root", "session_key": sessionKey},
		}}))
	}

	return map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
			"update_multi":     true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": "模型配置",
			},
			"template": "blue",
		},
		"elements": elements,
	}
}

func (a *App) updateGlobalModelConfig(mutate func(*config.CodexConfig), result codexrpc.ModelListResult) error {
	if a.cfg == nil {
		return fmt.Errorf("nil config")
	}
	mutate(&a.cfg.Codex)
	a.cfg.Codex.Model = strings.TrimSpace(a.cfg.Codex.Model)
	a.cfg.Codex.ReasoningEffort = strings.TrimSpace(a.cfg.Codex.ReasoningEffort)
	selectedModel := findModelEntry(result, a.cfg.Codex.Model)
	if !modelSupportsEffort(selectedModel, a.cfg.Codex.ReasoningEffort) {
		a.cfg.Codex.ReasoningEffort = ""
	}
	if err := a.cfg.Normalize(filepath.Dir(a.cfgPath)); err != nil {
		return err
	}
	return config.Save(a.cfgPath, a.cfg)
}

func (a *App) commandModel(msg *feishu.InboundMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.fetchModelList(ctx)
	if err != nil {
		return err
	}
	card := a.renderModelConfigCard(result, a.makeSessionKey(msg))
	_, err = a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) renderStatusCard(sessionKey string) map[string]any {
	var sess *state.Session
	if strings.TrimSpace(sessionKey) != "" {
		sess = a.store.GetSession(sessionKey)
	}
	buttons := []feishu.Button{
		{Text: "刷新", Type: "default", Value: map[string]any{"action": "menu.status", "session_key": sessionKey}},
		{Text: "返回菜单", Type: "default", Value: map[string]any{"action": "menu.root", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("Status", "blue", a.statusCardBody(sess), buttons)
}

func (a *App) statusCardBody(sess *state.Session) string {
	workspaceID := a.defaultWorkspaceID()
	threadLabel := "-"
	threadID := "-"
	status := "idle"
	queueLen := 0
	var ws *config.Workspace
	if sess != nil {
		if strings.TrimSpace(sess.WorkspaceID) != "" {
			workspaceID = sess.WorkspaceID
		}
		threadLabel = currentThreadLabel(sess)
		threadID = firstNonEmpty(sess.ActiveThreadID, "-")
		status = firstNonEmpty(sess.Status, "idle")
		queueLen = len(sess.Queue)
	}
	ws = config.FindWorkspace(a.cfg, workspaceID)
	model := configuredGlobalModel(a.cfg)
	if model == "" {
		model = "(follow app-server default)"
	}
	effort := configuredGlobalReasoningEffort(a.cfg)
	if effort == "" {
		effort = "(follow model default)"
	}
	workspaceSandbox := "-"
	workspacePolicy := "-"
	effectiveSandbox := "-"
	effectivePolicy := "-"
	if ws != nil {
		workspaceSandbox = firstNonEmpty(ws.SandboxMode, "-")
		workspacePolicy = firstNonEmpty(ws.ApprovalPolicy, "-")
		effectiveSandbox = effectiveThreadSandboxMode(sess, ws)
		effectivePolicy = effectiveThreadApprovalPolicy(sess, ws)
	}
	threadSandbox := renderThreadSettingValue("", "")
	threadPolicy := renderThreadSettingValue("", "")
	threadServiceTier := "-"
	if sess != nil {
		threadSandbox = renderThreadSettingValue(sess.ActiveThreadSandboxMode, "")
		threadPolicy = renderThreadSettingValue(sess.ActiveThreadApprovalPolicy, "")
		threadServiceTier = renderServiceTierValue(sess.ActiveThreadServiceTier)
	}
	return strings.Join([]string{
		"状态: `" + status + "`",
		"版本: `" + currentVersion() + "`",
		"工作区: `" + workspaceID + "`",
		"线程: " + threadLabel,
		"thread_id: `" + threadID + "`",
		"全局模型: `" + model + "`",
		"全局推理强度: `" + effort + "`",
		"quiet: `" + quietModeStatusText(a.quietModeEnabled()) + "`",
		"workspace sandbox: `" + workspaceSandbox + "`",
		"workspace policy: `" + workspacePolicy + "`",
		"thread sandbox: " + threadSandbox,
		"thread policy: " + threadPolicy,
		"thread service tier: " + threadServiceTier,
		"生效 sandbox: `" + effectiveSandbox + "`",
		"生效 policy: `" + effectivePolicy + "`",
		"queue_len: `" + fmt.Sprintf("%d", queueLen) + "`",
	}, "\n")
}

func (a *App) commandStatus(msg *feishu.InboundMessage) error {
	card := a.renderStatusCard(a.makeSessionKey(msg))
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}
