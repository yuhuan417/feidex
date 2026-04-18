package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const workspaceCloneProgressKeepLines = 6
const workspaceClonePatchInterval = 1200 * time.Millisecond

type workspaceCloneProgressReporter func(string)

var workspaceGitClone = func(ctx context.Context, repoURL, targetDir string, report workspaceCloneProgressReporter) error {
	cmd := exec.CommandContext(ctx, "git", "clone", "--progress", repoURL, targetDir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	var (
		mu         sync.Mutex
		output     []string
		streamErr  error
		streamWG   sync.WaitGroup
		recordLine = func(line string) {
			line = strings.TrimSpace(line)
			if line == "" {
				return
			}
			mu.Lock()
			output = append(output, line)
			if len(output) > 20 {
				output = append([]string(nil), output[len(output)-20:]...)
			}
			mu.Unlock()
			if report != nil {
				report(line)
			}
		}
	)
	consume := func(r io.Reader) {
		defer streamWG.Done()
		if err := readWorkspaceCloneOutput(r, recordLine); err != nil {
			if ctx.Err() != nil || errors.Is(err, os.ErrClosed) {
				return
			}
			mu.Lock()
			if streamErr == nil {
				streamErr = err
			}
			mu.Unlock()
		}
	}

	streamWG.Add(2)
	go consume(stdout)
	go consume(stderr)
	waitErr := cmd.Wait()
	streamWG.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	mu.Lock()
	message := strings.TrimSpace(strings.Join(output, "\n"))
	err = streamErr
	mu.Unlock()
	if waitErr != nil {
		if message == "" {
			message = waitErr.Error()
		}
		return fmt.Errorf("git clone failed: %s", message)
	}
	if err != nil {
		return err
	}
	return nil
}

type workspaceNewPayload struct {
	RootPath    string             `json:"root_path"`
	SelectedCWD string             `json:"selected_cwd"`
	DraftID     string             `json:"draft_id,omitempty"`
	AutoDraftID string             `json:"auto_draft_id,omitempty"`
	DraftName   string             `json:"draft_name,omitempty"`
	Picker      *pathPickerPayload `json:"picker,omitempty"`
}

type workspaceClonePayload struct {
	RootPath          string             `json:"root_path"`
	SelectedParentDir string             `json:"selected_parent_dir,omitempty"`
	RepoURL           string             `json:"repo_url,omitempty"`
	DraftID           string             `json:"draft_id,omitempty"`
	ErrorMessage      string             `json:"error_message,omitempty"`
	Picker            *pathPickerPayload `json:"picker,omitempty"`
}

type workspaceCloneTakeoverError struct {
	WorkspaceID string
	TargetDir   string
	Err         error
}

type workspaceCloneExistingDirError struct {
	WorkspaceID string
	TargetDir   string
}

type workspaceCloneProgressSnapshot struct {
	StartedAt      time.Time
	LastProgressAt time.Time
	State          string
	Lines          []string
}

type workspaceClonePlan struct {
	RepoName    string
	WorkspaceID string
	TargetDir   string
}

type workspaceCloneOperation struct {
	mu             sync.Mutex
	cancel         context.CancelFunc
	startedAt      time.Time
	lastProgressAt time.Time
	lastPatchAt    time.Time
	state          string
	lines          []string
}

func (e *workspaceCloneTakeoverError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("仓库已拉取到 %q，但创建工作区失败: %v", e.TargetDir, e.Err)
}

func (e *workspaceCloneTakeoverError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *workspaceCloneExistingDirError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("目标目录已存在: %s", e.TargetDir)
}

func newWorkspaceCloneOperation(cancel context.CancelFunc) *workspaceCloneOperation {
	now := time.Now()
	return &workspaceCloneOperation{
		cancel:         cancel,
		startedAt:      now,
		lastProgressAt: now,
		state:          "running",
	}
}

func (op *workspaceCloneOperation) snapshot() workspaceCloneProgressSnapshot {
	if op == nil {
		return workspaceCloneProgressSnapshot{}
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.snapshotLocked()
}

func (op *workspaceCloneOperation) snapshotLocked() workspaceCloneProgressSnapshot {
	snapshot := workspaceCloneProgressSnapshot{
		StartedAt:      op.startedAt,
		LastProgressAt: op.lastProgressAt,
		State:          op.state,
	}
	if len(op.lines) > 0 {
		snapshot.Lines = append([]string(nil), op.lines...)
	}
	return snapshot
}

func (op *workspaceCloneOperation) recordProgress(line string) (workspaceCloneProgressSnapshot, bool) {
	if op == nil {
		return workspaceCloneProgressSnapshot{}, false
	}
	line = strings.TrimSpace(line)
	now := time.Now()
	op.mu.Lock()
	defer op.mu.Unlock()
	if line != "" {
		if len(op.lines) == 0 || op.lines[len(op.lines)-1] != line {
			op.lines = append(op.lines, line)
			if len(op.lines) > workspaceCloneProgressKeepLines {
				op.lines = append([]string(nil), op.lines[len(op.lines)-workspaceCloneProgressKeepLines:]...)
			}
		}
		op.lastProgressAt = now
	}
	shouldPatch := op.lastPatchAt.IsZero() || now.Sub(op.lastPatchAt) >= workspaceClonePatchInterval
	if shouldPatch {
		op.lastPatchAt = now
	}
	return op.snapshotLocked(), shouldPatch
}

func (op *workspaceCloneOperation) requestCancel() workspaceCloneProgressSnapshot {
	if op == nil {
		return workspaceCloneProgressSnapshot{}
	}
	op.mu.Lock()
	if strings.TrimSpace(op.state) == "" || op.state == "running" {
		op.state = "cancelling"
	}
	op.lastPatchAt = time.Now()
	snapshot := op.snapshotLocked()
	cancel := op.cancel
	op.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return snapshot
}

func readWorkspaceCloneOutput(r io.Reader, emit func(string)) error {
	if r == nil {
		return nil
	}
	reader := bufio.NewReader(r)
	var buf strings.Builder
	flush := func() {
		line := strings.TrimSpace(buf.String())
		buf.Reset()
		if line == "" || emit == nil {
			return
		}
		emit(line)
	}
	for {
		b, err := reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				flush()
				return nil
			}
			return err
		}
		switch b {
		case '\r', '\n':
			flush()
		default:
			buf.WriteByte(b)
		}
	}
}

func workspaceNewPayloadFromPending(pending *state.PendingRequest) workspaceNewPayload {
	var payload workspaceNewPayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}

func workspaceClonePayloadFromPending(pending *state.PendingRequest) workspaceClonePayload {
	var payload workspaceClonePayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}

func (a *App) setWorkspaceCloneOperation(requestID string, op *workspaceCloneOperation) {
	if a == nil {
		return
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || op == nil {
		return
	}
	a.workspaceCloneMu.Lock()
	defer a.workspaceCloneMu.Unlock()
	if a.workspaceCloneOps == nil {
		a.workspaceCloneOps = map[string]*workspaceCloneOperation{}
	}
	if previous := a.workspaceCloneOps[requestID]; previous != nil && previous.cancel != nil && previous != op {
		previous.cancel()
	}
	a.workspaceCloneOps[requestID] = op
}

func (a *App) workspaceCloneOperation(requestID string) *workspaceCloneOperation {
	if a == nil {
		return nil
	}
	a.workspaceCloneMu.Lock()
	defer a.workspaceCloneMu.Unlock()
	if a.workspaceCloneOps == nil {
		return nil
	}
	return a.workspaceCloneOps[strings.TrimSpace(requestID)]
}

func (a *App) clearWorkspaceCloneOperation(requestID string) {
	if a == nil {
		return
	}
	a.workspaceCloneMu.Lock()
	defer a.workspaceCloneMu.Unlock()
	if a.workspaceCloneOps == nil {
		return
	}
	delete(a.workspaceCloneOps, strings.TrimSpace(requestID))
}

func (a *App) defaultWorkspaceNewRoot(ws *config.Workspace) string {
	return "/"
}

func (a *App) defaultWorkspaceCloneRoot(ws *config.Workspace) string {
	return "/"
}

func (a *App) renderWorkspaceNewCard(sessionKey, requestID string, payload workspaceNewPayload) map[string]any {
	if payload.Picker != nil {
		card, err := a.renderPathPickerCard(requestID, *payload.Picker)
		if err == nil {
			return card
		}
		payload.Picker = nil
	}
	selectedCWD := strings.TrimSpace(payload.SelectedCWD)
	if selectedCWD == "" {
		selectedCWD = payload.RootPath
	}
	card := newMarkdownBodyCard("新建工作区", "orange")
	body := "当前位置：主菜单 / workspace / new\n\n" +
		"已选目录: `" + firstNonEmpty(selectedCWD, "-") + "`\n" +
		"浏览根目录: `" + firstNonEmpty(strings.TrimSpace(payload.RootPath), "-") + "`\n\n" +
		"可以先选目录，再填写 `workspace_id` 和可选的 `name`。选完目录后会按目录名自动建议 `workspace_id`。点“确认”时才会校验 `workspace_id`。"
	appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": body})
	buttonRows := buildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text:  "选目录",
			Type:  "default",
			Name:  "workspace_new_pickdir",
			Value: map[string]any{"action": "workspace.new.pickdir", "request_id": requestID},
		},
		{
			Text:  "确认",
			Type:  "primary",
			Name:  "workspace_new_submit",
			Value: map[string]any{"action": "workspace.new.submit", "request_id": requestID},
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "workspace_new_cancel",
			Value: map[string]any{"action": "pending_form.cancel", "request_id": requestID},
		},
	})
	for idx, row := range buttonRows {
		columns := row["columns"].([]map[string]any)
		if len(columns) == 0 {
			continue
		}
		button := columns[0]["elements"].([]map[string]any)[0]
		if idx < 2 {
			button["form_action_type"] = "submit"
		}
	}
	workspaceIDInput := map[string]any{
		"tag":         "input",
		"name":        "workspace_id",
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "workspace_id"},
	}
	if value := strings.TrimSpace(payload.DraftID); value != "" {
		workspaceIDInput["default_value"] = value
	}
	workspaceNameInput := map[string]any{
		"tag":         "input",
		"name":        "workspace_name",
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "name（可选）"},
	}
	if value := strings.TrimSpace(payload.DraftName); value != "" {
		workspaceNameInput["default_value"] = value
	}
	form := map[string]any{
		"tag":                "form",
		"name":               "workspace_new_form",
		"direction":          "vertical",
		"horizontal_spacing": "8px",
		"vertical_spacing":   "8px",
		"elements":           append(append([]map[string]any{}, buttonRows...), workspaceIDInput, workspaceNameInput),
	}
	appendMarkdownBodyCardElement(card, form)
	return card
}

func (a *App) renderWorkspaceCloneCard(sessionKey, requestID string, payload workspaceClonePayload) map[string]any {
	if payload.Picker != nil {
		card, err := a.renderPathPickerCard(requestID, *payload.Picker)
		if err == nil {
			return card
		}
		payload.Picker = nil
	}
	var sess *state.Session
	if a.store != nil {
		sess = a.appState().session(sessionKey)
	}
	workspaceID := a.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(a.cfg, workspaceID)
	rootPath := firstNonEmpty(strings.TrimSpace(payload.RootPath), a.defaultWorkspaceCloneRoot(ws))
	parentDir := strings.TrimSpace(payload.SelectedParentDir)
	if parentDir == "" {
		parentDir = firstNonEmpty(strings.TrimSpace(a.defaultWorkspaceCloneParent(ws)), rootPath)
	}

	card := newMarkdownBodyCard("从仓库创建工作区", "orange")
	body := menuCardBody("workspace.clone",
		"当前工作区: `"+workspaceID+"`\n"+
			"已选父目录: `"+firstNonEmpty(parentDir, "-")+"`\n"+
			"浏览根目录: `"+firstNonEmpty(rootPath, "-")+"`\n\n"+
			"先填写 Git 地址，再按需调整父目录和可选 `workspace_id`。不填 `workspace_id` 时，会从仓库名自动推导。",
	)
	if errText := strings.TrimSpace(payload.ErrorMessage); errText != "" {
		body += "\n\n最近一次创建失败：\n" + errText + "\n\n请修正后重试。"
	}
	appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": body})

	repoURLInput := map[string]any{
		"tag":         "input",
		"name":        "repo_url",
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "git 地址，例如 https://github.com/org/repo.git"},
	}
	if value := strings.TrimSpace(payload.RepoURL); value != "" {
		repoURLInput["default_value"] = value
	}
	workspaceIDInput := map[string]any{
		"tag":         "input",
		"name":        "workspace_id",
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "workspace_id（可选）"},
	}
	if value := strings.TrimSpace(payload.DraftID); value != "" {
		workspaceIDInput["default_value"] = value
	}
	buttonRows := buildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text:  "选父目录",
			Type:  "default",
			Name:  "workspace_clone_pickdir",
			Value: map[string]any{"action": "workspace.clone.pickdir", "request_id": requestID},
		},
		{
			Text:  "确认",
			Type:  "primary",
			Name:  "workspace_clone_submit",
			Value: map[string]any{"action": "workspace.clone.submit", "request_id": requestID},
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "workspace_clone_cancel",
			Value: map[string]any{"action": "pending_form.cancel", "request_id": requestID},
		},
	})
	for idx, row := range buttonRows {
		columns := row["columns"].([]map[string]any)
		if len(columns) == 0 {
			continue
		}
		button := columns[0]["elements"].([]map[string]any)[0]
		if idx < 2 {
			button["form_action_type"] = "submit"
		}
	}
	form := map[string]any{
		"tag":                "form",
		"name":               "workspace_clone_form",
		"direction":          "vertical",
		"horizontal_spacing": "8px",
		"vertical_spacing":   "8px",
		"elements":           append(append([]map[string]any{repoURLInput}, buttonRows...), workspaceIDInput),
	}
	appendMarkdownBodyCardElement(card, form)
	return card
}

func (a *App) renderWorkspaceClonePreparingCard(requestID string, payload workspaceClonePayload, parentDir string, snapshot workspaceCloneProgressSnapshot) map[string]any {
	repoURL := strings.TrimSpace(payload.RepoURL)
	parentDir = firstNonEmpty(strings.TrimSpace(parentDir), strings.TrimSpace(payload.SelectedParentDir), "-")
	workspaceID := strings.TrimSpace(payload.DraftID)
	if workspaceID == "" {
		workspaceID = "将从仓库名自动推导"
	}
	statusLine := "正在从仓库创建工作区。"
	if snapshot.State == "cancelling" {
		statusLine = "正在取消仓库克隆。"
	}
	lines := []string{
		statusLine,
		"",
		"仓库: `" + firstNonEmpty(repoURL, "-") + "`",
		"父目录: `" + parentDir + "`",
		"workspace_id: `" + workspaceID + "`",
	}
	if !snapshot.StartedAt.IsZero() {
		lines = append(lines, "已运行: `"+strings.TrimSpace(strings.TrimPrefix(formatTurnElapsedLine(time.Since(snapshot.StartedAt)), "elapsed: "))+"`")
	}
	if len(snapshot.Lines) == 0 {
		lines = append(lines, "", "尚未收到 git 进度输出。")
	} else {
		lines = append(lines, "", "最近进度:", markdownCodeBlock(strings.Join(snapshot.Lines, "\n")))
	}
	lines = append(lines, "", "这张卡片会自动刷新。")
	var buttons []feishu.Button
	if snapshot.State != "cancelling" {
		buttons = []feishu.Button{
			{
				Text: "取消克隆",
				Type: "default",
				Value: map[string]any{
					"action":     "workspace.clone.cancel",
					"request_id": requestID,
				},
			},
		}
	}
	return a.feishu.SimpleStatusCard("从仓库创建工作区", "blue", strings.Join(lines, "\n"), buttons)
}

func (a *App) renderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir string) map[string]any {
	buttons := []feishu.Button{
		{
			Text: "转为新建工作区",
			Type: "primary",
			Value: map[string]any{
				"action":       "workspace.new.takeover",
				"session_key":  sessionKey,
				"workspace_id": strings.TrimSpace(workspaceID),
				"target_dir":   strings.TrimSpace(targetDir),
			},
		},
		{
			Text: "返回工作区管理",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.workspace",
				"session_key": sessionKey,
			},
		},
	}
	body := "已从仓库创建并切换到工作区 `" + workspaceID + "`\n\ncwd: `" + targetDir + "`"
	return a.feishu.SimpleStatusCard("工作区已创建", "green", body, buttons)
}

func (a *App) renderWorkspaceCloneManualHintCard(sessionKey, workspaceID, targetDir, errText string) map[string]any {
	lines := []string{
		"仓库已拉取，可手动接管。",
		"",
		"目录: `" + firstNonEmpty(strings.TrimSpace(targetDir), "-") + "`",
	}
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID != "" {
		lines = append(lines, "建议 workspace_id: `"+workspaceID+"`")
	}
	lines = append(lines, "", "自动创建或切换工作区失败。仓库目录已保留，可稍后通过 `/workspace new` 手动接管。")
	if errText = strings.TrimSpace(errText); errText != "" {
		lines = append(lines, "", "错误: "+errText)
	}
	buttons := []feishu.Button{
		{
			Text: "返回工作区管理",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.workspace",
				"session_key": sessionKey,
			},
		},
	}
	return a.feishu.SimpleStatusCard("仓库已拉取", "orange", strings.Join(lines, "\n"), buttons)
}

func (a *App) renderWorkspaceCloneCanceledCard(sessionKey string, payload workspaceClonePayload, parentDir string, snapshot workspaceCloneProgressSnapshot) map[string]any {
	repoURL := strings.TrimSpace(payload.RepoURL)
	parentDir = firstNonEmpty(strings.TrimSpace(parentDir), strings.TrimSpace(payload.SelectedParentDir), "-")
	workspaceID := strings.TrimSpace(payload.DraftID)
	if workspaceID == "" {
		workspaceID = "将从仓库名自动推导"
	}
	lines := []string{
		"已取消仓库克隆。",
		"",
		"仓库: `" + firstNonEmpty(repoURL, "-") + "`",
		"父目录: `" + parentDir + "`",
		"workspace_id: `" + workspaceID + "`",
		"",
		"如果目标目录有残留，请清理后重新发起。",
	}
	if len(snapshot.Lines) > 0 {
		lines = append(lines, "", "取消前最后进度:", markdownCodeBlock(strings.Join(snapshot.Lines, "\n")))
	}
	buttons := []feishu.Button{
		{
			Text: "返回工作区管理",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.workspace",
				"session_key": sessionKey,
			},
		},
	}
	return a.feishu.SimpleStatusCard("仓库克隆已取消", "grey", strings.Join(lines, "\n"), buttons)
}

func (a *App) patchWorkspaceCloneProgressCard(messageID, requestID string, payload workspaceClonePayload, parentDir string, snapshot workspaceCloneProgressSnapshot) {
	if a == nil || strings.TrimSpace(messageID) == "" {
		return
	}
	card := a.renderWorkspaceClonePreparingCard(requestID, payload, parentDir, snapshot)
	if err := a.feishu.PatchCard(context.Background(), messageID, card); err != nil {
		slog.Warn("workspace clone progress patch failed",
			"request_id", requestID,
			"message_id", messageID,
			"error", err,
		)
	}
}

func (a *App) noteWorkspaceCloneProgress(op *workspaceCloneOperation, requestID, messageID string, payload workspaceClonePayload, parentDir, line string) {
	if op == nil {
		return
	}
	snapshot, shouldPatch := op.recordProgress(line)
	if shouldPatch {
		a.patchWorkspaceCloneProgressCard(messageID, requestID, payload, parentDir, snapshot)
	}
}

func (a *App) finishWorkspaceCloneSubmit(ctx context.Context, op *workspaceCloneOperation, requestID, messageID, sessionKey, userID, chatID, chatType, parentDir string, payload workspaceClonePayload) {
	appState := a.appState()
	defer a.clearWorkspaceCloneOperation(requestID)
	slog.Debug("workspace clone started",
		"request_id", requestID,
		"message_id", messageID,
		"session_key", sessionKey,
		"repo_url", payload.RepoURL,
		"parent_dir", parentDir,
	)
	workspaceID, targetDir, err := a.cloneWorkspaceInParent(
		ctx,
		sessionKey,
		userID,
		chatID,
		chatType,
		payload.RepoURL,
		payload.DraftID,
		parentDir,
		func(line string) {
			a.noteWorkspaceCloneProgress(op, requestID, messageID, payload, parentDir, line)
		},
	)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Warn("workspace clone canceled",
				"request_id", requestID,
				"message_id", messageID,
				"session_key", sessionKey,
				"repo_url", payload.RepoURL,
				"parent_dir", parentDir,
			)
			payload.SelectedParentDir = parentDir
			payload.ErrorMessage = ""
			_ = appState.updatePending(requestID, func(req *state.PendingRequest) {
				req.Status = "resolved"
				req.PayloadJSON = mustJSON(payload)
				req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
			})
			if strings.TrimSpace(messageID) != "" {
				a.feishu.PatchCard(context.Background(), messageID, a.renderWorkspaceCloneCanceledCard(sessionKey, payload, parentDir, op.snapshot()))
			}
			return
		}
		var takeoverErr *workspaceCloneTakeoverError
		if errors.As(err, &takeoverErr) {
			slog.Warn("workspace clone needs manual takeover",
				"request_id", requestID,
				"message_id", messageID,
				"session_key", sessionKey,
				"repo_url", payload.RepoURL,
				"parent_dir", parentDir,
				"target_dir", takeoverErr.TargetDir,
				"workspace_id", takeoverErr.WorkspaceID,
				"error", takeoverErr.Err,
			)
			payload.SelectedParentDir = parentDir
			payload.DraftID = firstNonEmpty(strings.TrimSpace(payload.DraftID), strings.TrimSpace(takeoverErr.WorkspaceID))
			if takeoverErr.Err != nil {
				payload.ErrorMessage = takeoverErr.Err.Error()
			} else {
				payload.ErrorMessage = err.Error()
			}
			_ = appState.updatePending(requestID, func(req *state.PendingRequest) {
				req.Status = "resolved"
				req.PayloadJSON = mustJSON(payload)
				req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
			})
			if strings.TrimSpace(messageID) != "" {
				_ = a.feishu.PatchCard(context.Background(), messageID, a.renderWorkspaceCloneManualHintCard(sessionKey, payload.DraftID, takeoverErr.TargetDir, payload.ErrorMessage))
			}
			return
		}
		slog.Warn("workspace clone failed",
			"request_id", requestID,
			"message_id", messageID,
			"session_key", sessionKey,
			"repo_url", payload.RepoURL,
			"parent_dir", parentDir,
			"error", err,
		)
		payload.SelectedParentDir = parentDir
		payload.ErrorMessage = err.Error()
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) {
			req.Status = "pending"
			req.PayloadJSON = mustJSON(payload)
			req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
		})
		if strings.TrimSpace(messageID) != "" {
			_ = a.feishu.PatchCard(context.Background(), messageID, a.renderWorkspaceCloneCard(sessionKey, requestID, payload))
		}
		return
	}
	slog.Debug("workspace clone completed",
		"request_id", requestID,
		"message_id", messageID,
		"session_key", sessionKey,
		"workspace_id", workspaceID,
		"target_dir", targetDir,
	)
	payload.SelectedParentDir = parentDir
	payload.ErrorMessage = ""
	_ = appState.updatePending(requestID, func(req *state.PendingRequest) {
		req.Status = "resolved"
		req.PayloadJSON = mustJSON(payload)
	})
	if strings.TrimSpace(messageID) != "" {
		_ = a.feishu.PatchCard(context.Background(), messageID, a.renderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir))
	}
}

func (a *App) beginWorkspaceNew(msg *feishu.InboundMessage) error {
	sessionKey, _, ws := a.currentWorkspaceForMessage(msg)
	payload := workspaceNewPayload{
		RootPath: a.defaultWorkspaceNewRoot(ws),
		SelectedCWD: firstNonEmpty(func() string {
			if ws == nil {
				return ""
			}
			return strings.TrimSpace(ws.Cwd)
		}(), "/"),
	}
	return a.beginWorkspaceNewWithPayload(msg, sessionKey, payload)
}

func (a *App) beginWorkspaceNewWithPayload(msg *feishu.InboundMessage, sessionKey string, payload workspaceNewPayload) error {
	appState := a.appState()
	requestID, err := appState.nextLocalID("workspace")
	if err != nil {
		return err
	}
	card := a.renderWorkspaceNewCard(sessionKey, requestID, payload)
	msgID, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	if err != nil {
		return err
	}
	return appState.savePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "workspace_new",
		SessionKey:  sessionKey,
		OwnerUserID: msg.UserID,
		FeishuMsgID: msgID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
}

func (a *App) createWorkspaceNewPending(sessionKey, userID, feishuMsgID string, payload workspaceNewPayload) (string, error) {
	appState := a.appState()
	requestID, err := appState.nextLocalID("workspace")
	if err != nil {
		return "", err
	}
	if err := appState.savePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "workspace_new",
		SessionKey:  sessionKey,
		OwnerUserID: userID,
		FeishuMsgID: strings.TrimSpace(feishuMsgID),
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	}); err != nil {
		return "", err
	}
	return requestID, nil
}

func formValueString(values map[string]any, key string) (string, bool) {
	if len(values) == 0 {
		return "", false
	}
	raw, ok := values[key]
	if !ok {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v), true
	default:
		return strings.TrimSpace(fmt.Sprint(v)), true
	}
}

func mergeWorkspaceNewFormValues(payload workspaceNewPayload, values map[string]any) workspaceNewPayload {
	if value, ok := formValueString(values, "workspace_id"); ok {
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
	if value, ok := formValueString(values, "workspace_name"); ok {
		payload.DraftName = value
	}
	return payload
}

func mergeWorkspaceCloneFormValues(payload workspaceClonePayload, values map[string]any) workspaceClonePayload {
	if value, ok := formValueString(values, "repo_url"); ok {
		payload.RepoURL = value
	}
	if value, ok := formValueString(values, "workspace_id"); ok {
		payload.DraftID = value
	}
	return payload
}

func workspaceCloneRepoName(repoURL string) (string, error) {
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
		pathPart = firstNonEmpty(strings.TrimSpace(parsed.Path), strings.TrimSpace(parsed.Opaque))
	}
	base := strings.TrimSpace(strings.TrimSuffix(path.Base(strings.TrimSuffix(pathPart, "/")), ".git"))
	if base == "" || base == "." || base == "/" {
		return "", fmt.Errorf("无法从 git 地址推导仓库名")
	}
	return base, nil
}

func workspaceCloneDefaultID(repoName string) string {
	return workspaceSuggestedID(repoName)
}

func workspaceSuggestedIDFromDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(dir))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	return workspaceSuggestedID(base)
}

func workspaceSuggestedID(raw string) string {
	repoName := strings.ToLower(strings.TrimSpace(raw))
	var out strings.Builder
	lastDash := false
	for _, r := range repoName {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			out.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_':
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

func updateWorkspaceNewSuggestedID(payload workspaceNewPayload, selectedDir string) workspaceNewPayload {
	nextAuto := workspaceSuggestedIDFromDir(selectedDir)
	currentDraft := strings.TrimSpace(payload.DraftID)
	currentAuto := strings.TrimSpace(payload.AutoDraftID)
	if nextAuto != "" && (currentDraft == "" || currentDraft == currentAuto) {
		payload.DraftID = nextAuto
	}
	payload.AutoDraftID = nextAuto
	return payload
}

func (a *App) defaultWorkspaceCloneParent(ws *config.Workspace) string {
	if ws != nil && strings.TrimSpace(ws.Cwd) != "" {
		return filepath.Dir(strings.TrimSpace(ws.Cwd))
	}
	if strings.TrimSpace(a.cfgPath) != "" {
		return filepath.Dir(strings.TrimSpace(a.cfgPath))
	}
	return "."
}

func (a *App) prepareWorkspaceClone(repoURL, explicitID, parentDir string) (*workspaceClonePlan, error) {
	repoName, err := workspaceCloneRepoName(repoURL)
	if err != nil {
		return nil, err
	}
	workspaceID := strings.TrimSpace(explicitID)
	if workspaceID == "" {
		workspaceID = workspaceCloneDefaultID(repoName)
		if workspaceID == "" {
			return nil, fmt.Errorf("无法从 git 地址推导 workspace_id，请手动指定")
		}
	}
	if config.FindWorkspace(a.cfg, workspaceID) != nil {
		return nil, fmt.Errorf("workspace %q 已存在，请指定新的 workspace_id", workspaceID)
	}
	parentDir = strings.TrimSpace(parentDir)
	if parentDir == "" {
		return nil, fmt.Errorf("请先选择父目录")
	}
	targetName := repoName
	if strings.TrimSpace(explicitID) != "" {
		targetName = workspaceID
	}
	targetDir := filepath.Join(parentDir, targetName)
	if _, statErr := os.Stat(targetDir); statErr == nil {
		return nil, &workspaceCloneExistingDirError{
			WorkspaceID: workspaceID,
			TargetDir:   targetDir,
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	return &workspaceClonePlan{
		RepoName:    repoName,
		WorkspaceID: workspaceID,
		TargetDir:   targetDir,
	}, nil
}

func (a *App) cloneWorkspaceInParent(ctx context.Context, sessionKey, userID, chatID, chatType, repoURL, explicitID, parentDir string, report workspaceCloneProgressReporter) (string, string, error) {
	plan, err := a.prepareWorkspaceClone(repoURL, explicitID, parentDir)
	if err != nil {
		return "", "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := os.MkdirAll(filepath.Dir(plan.TargetDir), 0o755); err != nil {
		return "", "", err
	}
	if err := workspaceGitClone(ctx, strings.TrimSpace(repoURL), plan.TargetDir, report); err != nil {
		return "", "", err
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if err := a.createWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, plan.WorkspaceID, plan.WorkspaceID, plan.TargetDir); err != nil {
		return "", "", &workspaceCloneTakeoverError{
			WorkspaceID: plan.WorkspaceID,
			TargetDir:   plan.TargetDir,
			Err:         err,
		}
	}
	return plan.WorkspaceID, plan.TargetDir, nil
}

func (a *App) cloneWorkspaceAndSwitch(msg *feishu.InboundMessage, repoURL, explicitID string) error {
	return a.cloneWorkspaceAndSwitchInSelectedParent(msg, repoURL, explicitID, "")
}

func (a *App) cloneWorkspaceAndSwitchInSelectedParent(msg *feishu.InboundMessage, repoURL, explicitID, parentDir string) error {
	if msg == nil {
		return nil
	}
	sessionKey, _, ws := a.currentWorkspaceForMessage(msg)
	if strings.TrimSpace(parentDir) == "" {
		parentDir = a.defaultWorkspaceCloneParent(ws)
	}
	workspaceID, targetDir, err := a.cloneWorkspaceInParent(
		context.Background(),
		sessionKey,
		msg.UserID,
		msg.ChatID,
		msg.ChatType,
		repoURL,
		explicitID,
		parentDir,
		nil,
	)
	if err != nil {
		return err
	}
	reply := "已从仓库创建并切换到工作区 " + workspaceID + "\n" + "cwd: " + targetDir
	return a.feishu.ReplyText(context.Background(), msg.MessageID, reply, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}

func (a *App) createWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, id, name, cwd string) error {
	appState := a.appState()
	if config.FindWorkspace(a.cfg, id) != nil {
		return fmt.Errorf("workspace %q 已存在", id)
	}
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{
		ID:             id,
		Name:           name,
		Cwd:            cwd,
		Model:          "",
		ApprovalPolicy: "on-request",
		SandboxMode:    "workspace-write",
	})
	if err := a.cfg.Normalize(filepath.Dir(a.cfgPath)); err != nil {
		a.cfg.Workspaces = a.cfg.Workspaces[:len(a.cfg.Workspaces)-1]
		return err
	}
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		a.cfg.Workspaces = a.cfg.Workspaces[:len(a.cfg.Workspaces)-1]
		return err
	}
	sess := appState.session(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, ChatID: chatID, ChatType: chatType, OwnerUserID: userID}
	}
	switchSessionWorkspace(sess, id)
	if err := appState.saveSession(sess); err != nil {
		return err
	}
	if sessionHasInFlightSubmission(sess) {
		return nil
	}
	ws := config.FindWorkspace(a.cfg, id)
	if ws == nil {
		return nil
	}
	if _, err := a.ensureWorkspaceThreadBinding(sessionKey, sess, ws); err != nil {
		slog.Warn("workspace create thread binding failed",
			"session_key", sessionKey,
			"workspace_id", id,
			"cwd", cwd,
			"error", err,
		)
	}
	return nil
}

func (a *App) completeWorkspaceNewText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	appState := a.appState()
	payload := workspaceNewPayloadFromPending(pending)
	parts := strings.Fields(strings.TrimSpace(msg.Text))
	if len(parts) < 1 {
		return fmt.Errorf("格式错误，需发送: workspace_id [name]")
	}
	id := parts[0]
	cwd := strings.TrimSpace(payload.SelectedCWD)
	name := id
	if cwd == "" && len(parts) >= 2 {
		cwd = parts[1]
		if len(parts) > 2 {
			name = strings.Join(parts[2:], " ")
		}
	} else if len(parts) > 1 {
		name = strings.Join(parts[1:], " ")
	}
	if strings.TrimSpace(cwd) == "" {
		return fmt.Errorf("请先选择目录")
	}
	sessionKey := a.makeSessionKey(msg)
	if err := a.createWorkspaceAndSwitch(sessionKey, msg.UserID, msg.ChatID, msg.ChatType, id, name, cwd); err != nil {
		return err
	}
	_ = appState.updatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
	if pending.FeishuMsgID != "" {
		_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, a.feishu.SimpleStatusCard("工作区已创建", "green", "已创建并切换到工作区 `"+id+"`\n\ncwd: `"+cwd+"`", nil))
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "已创建并切换到工作区 "+id, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}
