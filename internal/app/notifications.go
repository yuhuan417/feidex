package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (a *App) handleNotification(method string, params json.RawMessage) {
	newCodexEventRouter(a).handleNotification(method, params)
}

func (a *App) onThreadTokenUsageUpdated(threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
	a.recordTurnTokenUsage(threadID, turnID, usage)
}

func (a *App) onTurnStartedNotification(threadID, turnID string) {
	newSubmissionWorkflow(a).onTurnStartedNotification(threadID, turnID)
}

func (a *App) handleServerRequest(req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).handleServerRequest(req)
}

func (a *App) onCommandApproval(req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onCommandApproval(req)
}

func (a *App) onFileApproval(req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onFileApproval(req)
}

func (a *App) onPermissionsApproval(req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onPermissionsApproval(req)
}

func (a *App) onToolUserInput(req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onToolUserInput(req)
}

func (a *App) onMcpElicitationRequest(req codexrpc.RequestEnvelope) {
	newCodexEventRouter(a).onMcpElicitationRequest(req)
}

func (a *App) updateSubmissionByTurn(threadID, turnID string, mutate func(*state.Submission)) {
	_, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	_ = a.store.UpdateSubmission(sub.ID, mutate)
}

func (a *App) finishTurn(threadID, turnID, status string) {
	newSubmissionWorkflow(a).finishTurn(threadID, turnID, status)
}

func (a *App) startNextSubmissionAsync(sessionKey, source string) {
	newSubmissionWorkflow(a).startNextSubmissionAsync(sessionKey, source)
}

func turnCompletionMessages(status, outputText, lastError string, sentOutput bool) (replyText, terminalText string) {
	outputText = strings.TrimSpace(outputText)
	lastError = strings.TrimSpace(lastError)
	if status == "completed" {
		return "", ""
	}
	if !sentOutput && outputText != "" {
		replyText = outputText
	}

	fallback := lastError
	if fallback == "" {
		switch status {
		case "interrupted":
			fallback = "任务已中断。"
		case "failed":
			fallback = "任务失败。"
		default:
			fallback = "任务已结束。"
		}
	}

	switch status {
	case "interrupted":
		terminalText = "任务已中断。"
	default:
		if replyText == "" {
			terminalText = fallback
		}
	}
	return replyText, terminalText
}

func (a *App) findSubmissionByTurn(threadID, turnID string) (string, *state.Submission) {
	if strings.TrimSpace(turnID) != "" {
		if sessionKey, sub := a.boundSubmissionForTurn(turnID); sub != nil {
			return sessionKey, sub
		}
		for _, sess := range a.store.AllSessions() {
			if turnID != "" && sess.ActiveTurnID == turnID {
				sub := a.store.GetSubmission(sess.ActiveSubmissionID)
				if sub != nil {
					return sess.Key, sub
				}
			}
		}
		return "", nil
	}
	if strings.TrimSpace(threadID) != "" {
		for _, sess := range a.store.AllSessions() {
			if sess == nil {
				continue
			}
			if strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
				continue
			}
			if strings.TrimSpace(sess.ActiveSubmissionID) == "" {
				continue
			}
			if strings.TrimSpace(sess.ActiveTurnID) != "" && strings.TrimSpace(turnID) != "" && sess.ActiveTurnID != turnID {
				continue
			}
			sub := a.store.GetSubmission(sess.ActiveSubmissionID)
			if sub != nil {
				return sess.Key, sub
			}
		}
	}
	return "", nil
}

func (a *App) sendStatusCardForSubmission(sub *state.Submission, msg *feishu.InboundMessage, status string) error {
	if a == nil || a.feishu == nil {
		return nil
	}
	card := a.renderSubmissionCard(sub, status)
	id, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	if err == nil && id != "" {
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.StatusCardID = id })
		a.recordMessageLink(id, "status_card", sub, "")
	}
	return err
}

func (a *App) refreshStatusCard(submissionID string) error {
	sub := a.store.GetSubmission(submissionID)
	if sub == nil || sub.StatusCardID == "" {
		return nil
	}
	return a.feishu.PatchCard(context.Background(), sub.StatusCardID, a.renderSubmissionCard(sub, sub.Status))
}

func (a *App) renderSubmissionCard(sub *state.Submission, status string) map[string]any {
	title := "排队中"
	color := "grey"
	switch status {
	case "running", "turn_in_progress":
		title = "运行中"
		color = "blue"
	case "waiting_approval":
		title = "等待审批"
		color = "orange"
	case "waiting_user_input":
		title = "等待输入"
		color = "orange"
	case "completed":
		title = "已完成"
		color = "green"
	case "failed":
		title = "失败"
		color = "red"
	case "interrupted":
		title = "已中断"
		color = "grey"
	}
	body := a.renderSubmissionCardBody(sub)
	buttons := []feishu.Button{{Text: "线程管理", Type: "default", Value: map[string]any{"action": "menu.thread", "session_key": sub.SessionKey}}}
	switch status {
	case "queued", "running", "turn_in_progress", "waiting_approval", "waiting_user_input":
		buttons = append([]feishu.Button{{Text: "中断", Type: "danger", Value: map[string]any{"action": "menu.interrupt", "session_key": sub.SessionKey}}}, buttons...)
	}
	return a.feishu.SimpleStatusCard(title, color, body, buttons)
}

func (a *App) renderSubmissionCardBody(sub *state.Submission) string {
	parts := make([]string, 0, 2)
	if input := strings.TrimSpace(submissionInputPreview(sub)); input != "" && input != "-" {
		parts = append(parts, "输入:\n"+a.prepareSubmissionCardMarkdown(sub, input))
	}
	if content := strings.TrimSpace(a.renderSubmissionLiveContent(sub)); content != "" {
		parts = append(parts, "内容:\n"+content)
	} else {
		parts = append(parts, "内容:\n"+submissionStatusPlaceholder(sub.Status))
	}
	return strings.Join(parts, "\n\n")
}

func (a *App) renderSubmissionLiveContent(sub *state.Submission) string {
	parts := make([]string, 0, 4)
	if plan := strings.TrimSpace(sub.PlanText); plan != "" {
		parts = append(parts, "计划:\n"+a.prepareSubmissionCardMarkdown(sub, plan))
	}
	if summary := strings.TrimSpace(sub.SummaryText); summary != "" {
		parts = append(parts, "摘要:\n"+a.prepareSubmissionCardMarkdown(sub, summary))
	}
	if command := strings.TrimSpace(sub.CommandText); command != "" {
		parts = append(parts, "命令输出:\n"+a.prepareSubmissionCardMarkdown(sub, command))
	}
	if output := strings.TrimSpace(sub.OutputText); output != "" {
		parts = append(parts, "回复:\n"+a.prepareSubmissionCardMarkdown(sub, output))
	}
	return strings.Join(parts, "\n\n")
}

func (a *App) prepareSubmissionCardMarkdown(sub *state.Submission, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
		text = sanitizeLocalMarkdownLinks(text, ws.Cwd)
	}
	return normalizeCardMarkdown(text)
}

func normalizeCardMarkdown(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	fenceOpen := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			lines[i] = "```"
			fenceOpen = !fenceOpen
		}
	}
	text = strings.Join(lines, "\n")
	if fenceOpen {
		text += "\n```"
	}
	return text
}

func submissionStatusPlaceholder(status string) string {
	switch status {
	case "queued":
		return "排队中..."
	case "running", "turn_in_progress":
		return "运行中..."
	case "waiting_approval":
		return "等待审批..."
	case "waiting_user_input":
		return "等待输入..."
	case "completed":
		return "任务已结束。"
	case "interrupted":
		return "任务已中断。"
	default:
		return "任务状态未知。"
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (a *App) sendApprovalCard(kind string, requestID json.RawMessage, threadID, turnID, itemID, body string) {
	a.sendApprovalCardWithPayload(kind, requestID, threadID, turnID, itemID, body, nil)
}

func (a *App) sendApprovalCardWithPayload(kind string, requestID json.RawMessage, threadID, turnID, itemID, body string, requestPayload map[string]any) {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		_ = a.codex.ReplyError(requestID, -32602, "no active session for approval")
		return
	}
	requestKey := requestIDKey(requestID)
	buttons := approvalButtons(kind, requestKey)
	card := a.renderApprovalCard(sessionKey, sub, "等待审批", "orange", strings.TrimSpace(body), buttons)
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err == nil {
		a.recordMessageLink(msgID, "approval_card", sub, requestKey)
		payload := map[string]any{}
		if strings.TrimSpace(body) != "" {
			payload["body"] = body
		}
		if len(requestPayload) > 0 {
			payload["request"] = requestPayload
		}
		_ = a.store.UpsertPending(&state.PendingRequest{
			ID:           requestKey,
			RequestIDRaw: requestIDStored(requestID),
			Kind:         kind,
			SessionKey:   sessionKey,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       itemID,
			OwnerUserID:  sub.UserID,
			FeishuMsgID:  msgID,
			PayloadJSON:  mustJSON(payload),
			Status:       "pending",
			CreatedAt:    time.Now().Unix(),
			ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
		})
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.Status = "waiting_approval" })
		_ = a.refreshStatusCard(sub.ID)
		return
	}
	_ = a.codex.ReplyError(requestID, -32603, err.Error())
}

func renderCommandApprovalBody(params map[string]any) string {
	lines := []string{"命令审批"}
	if command := strings.TrimSpace(firstNonEmpty(
		stringValue(params["command"]),
		stringValue(params["commandLine"]),
		stringValue(params["command_line"]),
	)); command != "" {
		lines = append(lines, markdownCodeBlock(command))
	}
	if cwd := strings.TrimSpace(firstNonEmpty(
		stringValue(params["cwd"]),
		stringValue(params["workingDirectory"]),
		stringValue(params["working_directory"]),
	)); cwd != "" {
		lines = append(lines, "工作目录: `"+strings.ReplaceAll(cwd, "`", "'")+"`")
	}
	if reason := strings.TrimSpace(stringValue(params["reason"])); reason != "" {
		if len(lines) > 1 {
			lines = append(lines, "")
		}
		lines = append(lines, "说明:", reason)
	}
	if len(lines) == 1 {
		if summary := strings.TrimSpace(truncatedApprovalRequestJSON(params)); summary != "" {
			lines = append(lines, markdownCodeBlock(summary))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func renderPermissionsApprovalBody(params map[string]any) string {
	lines := []string{"权限审批"}
	if reason := strings.TrimSpace(stringValue(params["reason"])); reason != "" {
		lines = append(lines, "说明:", reason)
	}
	permissions, _ := params["permissions"].(map[string]any)
	summary := summarizePermissions(permissions)
	if len(summary) > 0 {
		if len(lines) > 1 {
			lines = append(lines, "")
		}
		lines = append(lines, "权限摘要:")
		for _, line := range summary {
			lines = append(lines, "- "+line)
		}
	}
	if len(summary) == 0 {
		if rendered := strings.TrimSpace(truncate(prettyJSON(permissions), 800)); rendered != "" {
			if len(lines) > 1 {
				lines = append(lines, "")
			}
			lines = append(lines, "权限明细:", markdownCodeBlock(rendered))
		}
	}
	if len(summary) == 0 && len(lines) == 1 {
		if requestSummary := strings.TrimSpace(truncatedApprovalRequestJSON(params)); requestSummary != "" {
			lines = append(lines, markdownCodeBlock(requestSummary))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizePermissions(permissions map[string]any) []string {
	if len(permissions) == 0 {
		return nil
	}
	lines := []string{}
	add := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		lines = append(lines, label+": "+value)
	}

	if mode := strings.TrimSpace(firstNonEmpty(
		stringValue(permissions["mode"]),
		stringValue(permissions["access"]),
		stringValue(permissions["level"]),
	)); mode != "" {
		add("mode", "`"+strings.ReplaceAll(mode, "`", "'")+"`")
	}
	if scope := strings.TrimSpace(firstNonEmpty(
		stringValue(permissions["scope"]),
		stringValue(permissions["grant_scope"]),
		stringValue(permissions["session_scope"]),
	)); scope != "" {
		add("scope", "`"+strings.ReplaceAll(scope, "`", "'")+"`")
	}
	if sandbox := extractPermissionLabelledValue(permissions, "sandbox", "sandboxMode", "sandbox_mode", "type"); sandbox != "" {
		add("sandbox", "`"+strings.ReplaceAll(sandbox, "`", "'")+"`")
	}
	if network, ok := extractPermissionBool(permissions, "network", "networkAccess", "network_access", "allowNetwork", "allow_network"); ok {
		if network {
			add("network", "允许")
		} else {
			add("network", "禁止")
		}
	}
	if writable := collectPermissionPaths(permissions); len(writable) > 0 {
		const maxPaths = 6
		shown := writable
		if len(shown) > maxPaths {
			shown = shown[:maxPaths]
		}
		value := "`" + strings.Join(shown, "`, `") + "`"
		if len(writable) > maxPaths {
			value += fmt.Sprintf(" 等 %d 项", len(writable))
		}
		add("paths", value)
	}
	if len(lines) > 0 {
		return lines
	}
	flat := flattenPermissionScalars("", permissions, 0)
	if len(flat) == 0 {
		return nil
	}
	sort.Strings(flat)
	if len(flat) > 8 {
		flat = flat[:8]
	}
	return flat
}

func extractPermissionLabelledValue(root map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringValue(root[key])); value != "" {
			return value
		}
	}
	for _, key := range keys {
		if nested, ok := root[key].(map[string]any); ok {
			if value := strings.TrimSpace(firstNonEmpty(
				stringValue(nested["type"]),
				stringValue(nested["mode"]),
				stringValue(nested["value"]),
			)); value != "" {
				return value
			}
		}
	}
	return ""
}

func extractPermissionBool(root map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if value, ok := boolValue(root[key]); ok {
			return value, true
		}
	}
	return false, false
}

func collectPermissionPaths(root map[string]any) []string {
	values := []string{}
	seen := map[string]struct{}{}
	var add func(string)
	add = func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		values = append(values, path)
	}
	var walk func(any, int)
	walk = func(current any, depth int) {
		if current == nil || depth > 3 {
			return
		}
		switch x := current.(type) {
		case string:
			add(x)
		case []any:
			for _, item := range x {
				walk(item, depth+1)
			}
		case map[string]any:
			for _, key := range []string{"paths", "roots", "writable_roots", "writableRoots", "allowed_paths", "allowedPaths"} {
				if nested, ok := x[key]; ok {
					walk(nested, depth+1)
				}
			}
		}
	}
	walk(root, 0)
	sort.Strings(values)
	return values
}

func boolValue(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "yes", "1", "allow", "allowed":
			return true, true
		case "false", "no", "0", "deny", "denied":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func flattenPermissionScalars(prefix string, value any, depth int) []string {
	if depth > 3 || value == nil {
		return nil
	}
	lines := []string{}
	switch x := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			nextPrefix := key
			if prefix != "" {
				nextPrefix = prefix + "." + key
			}
			lines = append(lines, flattenPermissionScalars(nextPrefix, x[key], depth+1)...)
		}
	case []any:
		for i, item := range x {
			lines = append(lines, flattenPermissionScalars(fmt.Sprintf("%s[%d]", prefix, i), item, depth+1)...)
		}
	case string:
		if trimmed := strings.TrimSpace(x); trimmed != "" {
			lines = append(lines, prefix+" = `"+strings.ReplaceAll(trimmed, "`", "'")+"`")
		}
	case bool:
		lines = append(lines, fmt.Sprintf("%s = %t", prefix, x))
	case float64:
		lines = append(lines, fmt.Sprintf("%s = %v", prefix, x))
	}
	return lines
}

type approvalFileEntry struct {
	Path string
	Kind string
}

func renderFileApprovalBody(params map[string]any) string {
	lines := []string{"文件变更审批"}
	entries := collectFileApprovalEntries(params)
	if len(entries) > 0 {
		lines = append(lines, fmt.Sprintf("%d 个文件：", len(entries)))
		const maxEntries = 8
		for i, entry := range entries {
			if i >= maxEntries {
				lines = append(lines, fmt.Sprintf("- 还有 %d 个文件未展开", len(entries)-maxEntries))
				break
			}
			line := entry.Path
			if strings.TrimSpace(entry.Kind) != "" {
				line += " (" + entry.Kind + ")"
			}
			lines = append(lines, "- "+line)
		}
	}
	if reason := strings.TrimSpace(stringValue(params["reason"])); reason != "" {
		if len(lines) > 1 {
			lines = append(lines, "")
		}
		lines = append(lines, "说明:", reason)
	}
	if len(entries) == 0 {
		if summary := strings.TrimSpace(truncatedApprovalRequestJSON(params)); summary != "" {
			if len(lines) > 1 {
				lines = append(lines, "")
			}
			lines = append(lines, "请求摘要:", markdownCodeBlock(summary))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func collectFileApprovalEntries(value any) []approvalFileEntry {
	out := []approvalFileEntry{}
	seen := map[string]struct{}{}
	var add func(approvalFileEntry)
	add = func(entry approvalFileEntry) {
		entry.Path = strings.TrimSpace(entry.Path)
		entry.Kind = strings.TrimSpace(entry.Kind)
		if entry.Path == "" {
			return
		}
		key := entry.Path + "\x00" + entry.Kind
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, entry)
	}
	var walk func(any, int)
	walk = func(current any, depth int) {
		if depth > 3 || current == nil {
			return
		}
		switch x := current.(type) {
		case string:
			add(approvalFileEntry{Path: x})
		case []any:
			for _, item := range x {
				add(parseApprovalFileEntry(item))
			}
		case map[string]any:
			for _, key := range []string{"changes", "fileChanges", "file_changes", "files", "paths", "filePaths", "file_paths"} {
				if arr, ok := x[key].([]any); ok {
					walk(arr, depth+1)
				}
			}
			for _, key := range []string{"path", "file", "filePath", "file_path", "targetPath", "target_path"} {
				if path := strings.TrimSpace(stringValue(x[key])); path != "" {
					add(parseApprovalFileEntry(x))
					break
				}
			}
			for _, key := range []string{"item", "payload", "change", "fileChange", "file_change", "details", "result"} {
				if nested, ok := x[key]; ok {
					walk(nested, depth+1)
				}
			}
		}
	}
	walk(value, 0)
	return out
}

func parseApprovalFileEntry(value any) approvalFileEntry {
	switch x := value.(type) {
	case string:
		return approvalFileEntry{Path: strings.TrimSpace(x)}
	case map[string]any:
		oldPath := strings.TrimSpace(firstNonEmpty(stringValue(x["oldPath"]), stringValue(x["old_path"])))
		newPath := strings.TrimSpace(firstNonEmpty(stringValue(x["newPath"]), stringValue(x["new_path"])))
		path := strings.TrimSpace(firstNonEmpty(
			stringValue(x["path"]),
			stringValue(x["file"]),
			stringValue(x["filePath"]),
			stringValue(x["file_path"]),
			stringValue(x["targetPath"]),
			stringValue(x["target_path"]),
			stringValue(x["name"]),
		))
		if oldPath != "" && newPath != "" && oldPath != newPath {
			path = oldPath + " -> " + newPath
		}
		kind := strings.TrimSpace(firstNonEmpty(
			stringValue(x["kind"]),
			stringValue(x["changeType"]),
			stringValue(x["change_type"]),
			stringValue(x["op"]),
			stringValue(x["action"]),
			stringValue(x["status"]),
			stringValue(x["type"]),
		))
		return approvalFileEntry{Path: path, Kind: kind}
	default:
		return approvalFileEntry{}
	}
}

func truncatedApprovalRequestJSON(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	trimmed := map[string]any{}
	for key, value := range params {
		switch key {
		case "threadId", "turnId", "itemId", "reason":
			continue
		default:
			trimmed[key] = value
		}
	}
	if len(trimmed) == 0 {
		return ""
	}
	return truncate(prettyJSON(trimmed), 800)
}

func approvalButtons(kind, requestKey string) []feishu.Button {
	return []feishu.Button{
		{Text: "允许一次", Type: "primary", Value: map[string]any{"action": "approval." + kind + ".accept", "request_id": requestKey}},
		{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval." + kind + ".accept_session", "request_id": requestKey}},
		{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval." + kind + ".decline", "request_id": requestKey}},
	}
}

func (a *App) sendPermissionsCard(requestID json.RawMessage, threadID, turnID, itemID, body string, permissions map[string]any) {
	a.sendPermissionsCardWithPayload(requestID, threadID, turnID, itemID, body, permissions, nil)
}

func (a *App) sendPermissionsCardWithPayload(requestID json.RawMessage, threadID, turnID, itemID, body string, permissions map[string]any, requestPayload map[string]any) {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		_ = a.codex.ReplyError(requestID, -32602, "no active session for permissions approval")
		return
	}
	requestKey := requestIDKey(requestID)
	card := a.renderApprovalCard(sessionKey, sub, "权限请求", "orange", strings.TrimSpace(body), []feishu.Button{
		{Text: "本次允许", Type: "primary", Value: map[string]any{"action": "approval.permissions.accept_turn", "request_id": requestKey}},
		{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval.permissions.accept_session", "request_id": requestKey}},
	})
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err == nil {
		a.recordMessageLink(msgID, "permissions_card", sub, requestKey)
		payload := map[string]any{"permissions": permissions}
		if strings.TrimSpace(body) != "" {
			payload["body"] = body
		}
		if len(requestPayload) > 0 {
			payload["request"] = requestPayload
		}
		_ = a.store.UpsertPending(&state.PendingRequest{
			ID:           requestKey,
			RequestIDRaw: requestIDStored(requestID),
			Kind:         "permissions",
			SessionKey:   sessionKey,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       itemID,
			OwnerUserID:  sub.UserID,
			FeishuMsgID:  msgID,
			PayloadJSON:  mustJSON(payload),
			Status:       "pending",
			CreatedAt:    time.Now().Unix(),
			ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
		})
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.Status = "waiting_approval" })
		_ = a.refreshStatusCard(sub.ID)
		return
	}
	_ = a.codex.ReplyError(requestID, -32603, err.Error())
}

func (a *App) renderApprovalCard(_ string, _ *state.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
	return a.feishu.SimpleStatusCard(title, color, strings.TrimSpace(body), buttons)
}

func (a *App) sendUserInputCard(requestID json.RawMessage, payload toolUserInputPayload) {
	sessionKey, sub := a.findSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil || len(payload.Questions) == 0 {
		_ = a.codex.ReplyError(requestID, -32602, "no active session for request_user_input")
		return
	}
	q := payload.Questions[0]
	buttons := make([]feishu.Button, 0, len(q.Options))
	for _, opt := range q.Options {
		buttons = append(buttons, feishu.Button{
			Text: opt.Label,
			Type: "default",
			Value: map[string]any{
				"action":      "user_input.answer",
				"request_id":  requestIDKey(requestID),
				"question_id": q.ID,
				"answer":      opt.Label,
			},
		})
	}
	card := a.feishu.SimpleStatusCard("需要补充输入", "orange", q.Question, buttons)
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err == nil {
		requestKey := requestIDKey(requestID)
		a.recordMessageLink(msgID, "user_input_card", sub, requestKey)
		_ = a.store.UpsertPending(&state.PendingRequest{
			ID:           requestKey,
			RequestIDRaw: requestIDStored(requestID),
			Kind:         "tool_request_user_input",
			SessionKey:   sessionKey,
			ThreadID:     payload.ThreadID,
			TurnID:       payload.TurnID,
			ItemID:       payload.ItemID,
			OwnerUserID:  sub.UserID,
			FeishuMsgID:  msgID,
			PayloadJSON:  mustJSON(payload),
			Status:       "pending",
			CreatedAt:    time.Now().Unix(),
			ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
		})
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.Status = "waiting_user_input" })
		_ = a.refreshStatusCard(sub.ID)
		return
	}
	_ = a.codex.ReplyError(requestID, -32603, err.Error())
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
