package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestRenderPathPickerCardShowsDropdownAndShortButtons(t *testing.T) {
	a, _, _ := newTestApp(t)
	root := t.TempDir()
	current := filepath.Join(root, "work")
	if err := os.MkdirAll(filepath.Join(current, "dir-a"), 0o755); err != nil {
		t.Fatalf("Mkdir(dir-a) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(current, "file-a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile(file-a.txt) error = %v", err)
	}
	card, err := a.renderPathPickerCard("path-1", pathPickerPayload{
		Mode:        pathPickerModeDirectory,
		Style:       pathPickerStyleDropdown,
		RootPath:    root,
		CurrentPath: current,
	})
	if err != nil {
		t.Fatalf("renderPathPickerCard() error = %v", err)
	}
	if !cardHasTag(card, "select_static") {
		t.Fatalf("path picker card missing select_static: %#v", card)
	}
	body := cardMarkdownContent(t, card)
	for _, want := range []string{"浏览根目录", "当前目录", "已隐藏文件: `1`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("path picker body = %q, want %q", body, want)
		}
	}
	for _, want := range []string{"上一级", "确认", "取消"} {
		if !cardHasButtonText(card, want) {
			t.Fatalf("path picker missing button %q", want)
		}
	}
}

func TestPathPickerDropdownFlowSelectsFile(t *testing.T) {
	a, _, _ := newTestApp(t)
	root := a.cfg.Workspaces[0].Cwd
	subdir := filepath.Join(root, "child")
	filePath := filepath.Join(subdir, "note.txt")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("Mkdir(child) error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte("note"), 0o644); err != nil {
		t.Fatalf("WriteFile(note.txt) error = %v", err)
	}
	payload := pathPickerPayload{
		Mode:        pathPickerModeFile,
		Style:       pathPickerStyleDropdown,
		RootPath:    root,
		CurrentPath: root,
	}
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "path-1",
		Kind:        pathPickerKind,
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(payload),
		CreatedAt:   time.Now().Unix(),
	}); err != nil {
		t.Fatalf("UpsertPending(path-1) error = %v", err)
	}

	resp, err := a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "path-1"},
		Option:      encodePathPickerOption(pathPickerEntry{Name: "child", Path: subdir, IsDir: true}),
	}, "path_picker.dropdown")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("dropdown open dir = %#v, %v", resp, err)
	}
	pending := a.store.PendingByID("path-1")
	var gotPayload pathPickerPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &gotPayload); err != nil {
		t.Fatalf("Unmarshal(payload after dir) error = %v", err)
	}
	if filepath.Clean(gotPayload.CurrentPath) != filepath.Clean(subdir) {
		t.Fatalf("current path after dir = %q, want %q", gotPayload.CurrentPath, subdir)
	}

	resp, err = a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "path-1"},
		Option:      encodePathPickerOption(pathPickerEntry{Name: "note.txt", Path: filePath, IsDir: false}),
	}, "path_picker.dropdown")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("dropdown select file = %#v, %v", resp, err)
	}
	pending = a.store.PendingByID("path-1")
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &gotPayload); err != nil {
		t.Fatalf("Unmarshal(payload after file) error = %v", err)
	}
	if filepath.Clean(gotPayload.SelectedPath) != filepath.Clean(filePath) {
		t.Fatalf("selected path after file = %q, want %q", gotPayload.SelectedPath, filePath)
	}

	resp, err = a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "path-1"},
	}, "path_picker.confirm")
	if err != nil {
		t.Fatalf("confirm file selection error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("confirm file selection response = %#v, want success", resp)
	}
	if pending := a.store.PendingByID("path-1"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after confirm = %+v, want resolved", pending)
	}
}

func TestPathPickerDirectoryConfirmUsesCurrentPath(t *testing.T) {
	a, _, _ := newTestApp(t)
	root := a.cfg.Workspaces[0].Cwd
	subdir := filepath.Join(root, "repo")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("Mkdir(repo) error = %v", err)
	}
	payload := pathPickerPayload{
		Mode:        pathPickerModeDirectory,
		Style:       pathPickerStyleDropdown,
		RootPath:    root,
		CurrentPath: root,
	}
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "path-2",
		Kind:        pathPickerKind,
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(payload),
		CreatedAt:   time.Now().Unix(),
	}); err != nil {
		t.Fatalf("UpsertPending(path-2) error = %v", err)
	}

	resp, err := a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "path-2", "path": subdir},
	}, "path_picker.open")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("open directory response = %#v, %v", resp, err)
	}

	resp, err = a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "path-2"},
	}, "path_picker.confirm")
	if err != nil {
		t.Fatalf("confirm directory selection error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("confirm directory selection response = %#v, want success", resp)
	}
	if pending := a.store.PendingByID("path-2"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after directory confirm = %+v, want resolved", pending)
	}
}

func TestWorkspaceNewPickDirAndSubmit(t *testing.T) {
	a, _, _ := newTestApp(t)
	root := t.TempDir()
	current := filepath.Join(root, "repo")
	target := filepath.Join(root, "new-project")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("MkdirAll(current) error = %v", err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll(target) error = %v", err)
	}
	a.cfg.Workspaces[0].Cwd = current
	payload := workspaceNewPayload{
		RootPath:    "/",
		SelectedCWD: current,
	}
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "workspace-1",
		Kind:        "workspace_new",
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(payload),
	}); err != nil {
		t.Fatalf("UpsertPending(workspace-1) error = %v", err)
	}

	resp, err := a.completeWorkspaceNewPickDir(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "workspace-1"},
		FormValue:   map[string]any{"workspace_id": "repo", "workspace_name": "Repo"},
	})
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("completeWorkspaceNewPickDir() = %#v, %v", resp, err)
	}
	pending := a.store.PendingByID("workspace-1")
	gotPayload := workspaceNewPayloadFromPending(pending)
	if gotPayload.Picker == nil || gotPayload.DraftID != "repo" || gotPayload.DraftName != "Repo" {
		t.Fatalf("workspace payload after pickdir = %+v", gotPayload)
	}

	resp, err = a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "workspace-1"},
		Option:      encodePathPickerOption(pathPickerEntry{Name: "new-project", Path: target, IsDir: true}),
	}, "path_picker.dropdown")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("workspace picker dropdown = %#v, %v", resp, err)
	}
	resp, err = a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "workspace-1"},
	}, "path_picker.confirm")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("workspace picker confirm = %#v, %v", resp, err)
	}
	pending = a.store.PendingByID("workspace-1")
	gotPayload = workspaceNewPayloadFromPending(pending)
	if gotPayload.Picker != nil || filepath.Clean(gotPayload.SelectedCWD) != filepath.Clean(target) || gotPayload.DraftID != "repo" || gotPayload.DraftName != "Repo" {
		t.Fatalf("workspace payload after confirm = %+v", gotPayload)
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	inputs := workspaceNewFormInputs(t, cardData)
	if got, _ := inputs["workspace_id"]["default_value"].(string); got != "repo" {
		t.Fatalf("workspace_id default_value = %q, want repo", got)
	}
	if got, _ := inputs["workspace_id"]["required"].(bool); got {
		t.Fatalf("workspace_id required = %v, want false", got)
	}
	if got, _ := inputs["workspace_name"]["default_value"].(string); got != "Repo" {
		t.Fatalf("workspace_name default_value = %q, want Repo", got)
	}
	buttons := workspaceNewFormButtons(t, cardData)
	if got, _ := buttons["workspace_new_pickdir"]["form_action_type"].(string); got != "submit" {
		t.Fatalf("workspace_new_pickdir form_action_type = %q, want submit", got)
	}
	if got, _ := buttons["workspace_new_submit"]["form_action_type"].(string); got != "submit" {
		t.Fatalf("workspace_new_submit form_action_type = %q, want submit", got)
	}

	resp, err = a.completeWorkspaceNewSubmit(&feishu.CardAction{
		UserID:      "user-1",
		ChatID:      "chat-1",
		ActionValue: map[string]any{"request_id": "workspace-1"},
		FormValue:   map[string]any{},
	})
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("completeWorkspaceNewSubmit() = %#v, %v", resp, err)
	}
	if ws := config.FindWorkspace(a.cfg, "repo"); ws == nil || filepath.Clean(ws.Cwd) != filepath.Clean(target) {
		t.Fatalf("created workspace = %+v, want cwd %q", ws, target)
	}
}

func TestWorkspaceNewPickDirSuggestsWorkspaceIDFromDirectory(t *testing.T) {
	a, _, _ := newTestApp(t)
	root := t.TempDir()
	target := filepath.Join(root, "Feature Repo")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll(target) error = %v", err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "workspace-suggest-1",
		Kind:        "workspace_new",
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(workspaceNewPayload{
			RootPath:    "/",
			SelectedCWD: root,
		}),
	}); err != nil {
		t.Fatalf("UpsertPending(workspace-suggest-1) error = %v", err)
	}

	resp, err := a.completeWorkspaceNewPickDir(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "workspace-suggest-1"},
	})
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("completeWorkspaceNewPickDir() = %#v, %v", resp, err)
	}
	resp, err = a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "workspace-suggest-1"},
		Option:      encodePathPickerOption(pathPickerEntry{Name: "Feature Repo", Path: target, IsDir: true}),
	}, "path_picker.dropdown")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("workspace picker dropdown = %#v, %v", resp, err)
	}
	resp, err = a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "workspace-suggest-1"},
	}, "path_picker.confirm")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("workspace picker confirm = %#v, %v", resp, err)
	}

	pending := a.store.PendingByID("workspace-suggest-1")
	gotPayload := workspaceNewPayloadFromPending(pending)
	if gotPayload.DraftID != "feature-repo" || gotPayload.AutoDraftID != "feature-repo" {
		t.Fatalf("workspace payload after suggest = %+v", gotPayload)
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	inputs := workspaceNewFormInputs(t, cardData)
	if got, _ := inputs["workspace_id"]["default_value"].(string); got != "feature-repo" {
		t.Fatalf("workspace_id default_value = %q, want feature-repo", got)
	}
}

func TestWorkspaceNewSubmitExistingWorkspacePromptsSwitch(t *testing.T) {
	a, _, _ := newTestApp(t)
	existingDir := t.TempDir()
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "repo", Cwd: existingDir})

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "workspace-existing-1",
		Kind:        "workspace_new",
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(workspaceNewPayload{
			RootPath:    "/",
			SelectedCWD: existingDir,
			DraftID:     "repo",
		}),
	}); err != nil {
		t.Fatalf("UpsertPending(workspace-existing-1) error = %v", err)
	}

	resp, err := a.completeWorkspaceNewSubmit(&feishu.CardAction{
		UserID:      "user-1",
		ChatID:      "chat-1",
		ActionValue: map[string]any{"request_id": "workspace-existing-1"},
		FormValue: map[string]any{
			"workspace_id":   "repo",
			"workspace_name": "Repo",
		},
	})
	if err != nil || resp == nil || resp.Card == nil || resp.Toast == nil {
		t.Fatalf("completeWorkspaceNewSubmit() = %#v, %v", resp, err)
	}
	if resp.Toast.Type != "info" || !strings.Contains(resp.Toast.Content, "目录一致") {
		t.Fatalf("workspace new existing workspace toast = %#v, want switch hint", resp.Toast)
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	if body := cardMarkdownContent(t, cardData); !strings.Contains(body, existingDir) || !strings.Contains(body, "是否直接切换到这个工作区") {
		t.Fatalf("workspace existing switch body = %q", body)
	}
	if !cardHasButtonText(cardData, "切换到该工作区") {
		t.Fatalf("workspace existing switch card buttons = %#v", cardButtonsForTest(cardData))
	}
	if pending := a.store.PendingByID("workspace-existing-1"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("workspace new pending after existing workspace hint = %+v, want resolved", pending)
	}
}

func TestWorkspaceFormOrdering(t *testing.T) {
	a, _, _ := newTestApp(t)

	newForm := workspaceNewForm(t, a.renderWorkspaceNewCard("sess-1", "req-new", workspaceNewPayload{
		RootPath:    "/",
		SelectedCWD: a.cfg.Workspaces[0].Cwd,
	}))
	newElements, _ := newForm["elements"].([]map[string]any)
	if got, _ := newElements[0]["tag"].(string); got != "column_set" {
		t.Fatalf("workspace new first form element = %q, want column_set", got)
	}

	cloneForm := workspaceCloneForm(t, a.renderWorkspaceCloneCard("sess-1", "req-clone", workspaceClonePayload{
		RootPath:          "/",
		SelectedParentDir: filepath.Dir(a.cfg.Workspaces[0].Cwd),
	}))
	cloneElements, _ := cloneForm["elements"].([]map[string]any)
	if got, _ := cloneElements[0]["tag"].(string); got != "input" {
		t.Fatalf("workspace clone first form element = %q, want input", got)
	}
	if got, _ := cloneElements[0]["name"].(string); got != "repo_url" {
		t.Fatalf("workspace clone first form field = %q, want repo_url", got)
	}
}

func TestWorkspaceCloneSubmitFromMenuRunsAsyncAndPatchesSuccess(t *testing.T) {
	a, ff, fc := newTestApp(t)
	baseDir := t.TempDir()
	currentDir := filepath.Join(baseDir, "current")
	parentDir := filepath.Join(baseDir, "parents")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(currentDir) error = %v", err)
	}
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(parentDir) error = %v", err)
	}
	a.cfg.Workspaces[0].Cwd = currentDir

	origClone := workspaceGitClone
	defer func() { workspaceGitClone = origClone }()

	started := make(chan struct{})
	release := make(chan struct{})
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})
	var gotRepoURL string
	var gotTargetDir string
	workspaceGitClone = func(_ context.Context, repoURL, targetDir string, _ workspaceCloneProgressReporter) error {
		gotRepoURL = repoURL
		gotTargetDir = targetDir
		close(started)
		<-release
		return os.MkdirAll(filepath.Join(targetDir, ".git"), 0o755)
	}

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "thread/list":
			*out.(*codexrpc.ThreadListResult) = codexrpc.ThreadListResult{}
			return nil
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-clone"
			result.Thread.Name = "Clone Thread"
			result.Thread.Preview = "Clone Preview"
			return nil
		default:
			return nil
		}
	}

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "workspace-clone-1",
		Kind:        "workspace_clone",
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		FeishuMsgID: "msg-1",
		Status:      "pending",
		PayloadJSON: mustJSON(workspaceClonePayload{
			RootPath:          "/",
			SelectedParentDir: baseDir,
		}),
	}); err != nil {
		t.Fatalf("UpsertPending(workspace-clone-1) error = %v", err)
	}

	resp, err := a.completeWorkspaceClonePickDir(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "workspace-clone-1"},
		FormValue: map[string]any{
			"repo_url":     "git@github.com:example/repo.git",
			"workspace_id": "repo-copy",
		},
	})
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("completeWorkspaceClonePickDir() = %#v, %v", resp, err)
	}
	pending := a.store.PendingByID("workspace-clone-1")
	gotPayload := workspaceClonePayloadFromPending(pending)
	if gotPayload.Picker == nil || gotPayload.RepoURL != "git@github.com:example/repo.git" || gotPayload.DraftID != "repo-copy" {
		t.Fatalf("workspace clone payload after pickdir = %+v", gotPayload)
	}

	resp, err = a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "workspace-clone-1"},
		Option:      encodePathPickerOption(pathPickerEntry{Name: "parents", Path: parentDir, IsDir: true}),
	}, "path_picker.dropdown")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("workspace clone picker dropdown = %#v, %v", resp, err)
	}
	resp, err = a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "workspace-clone-1"},
	}, "path_picker.confirm")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("workspace clone picker confirm = %#v, %v", resp, err)
	}
	pending = a.store.PendingByID("workspace-clone-1")
	gotPayload = workspaceClonePayloadFromPending(pending)
	if gotPayload.Picker != nil || filepath.Clean(gotPayload.SelectedParentDir) != filepath.Clean(parentDir) || gotPayload.RepoURL != "git@github.com:example/repo.git" || gotPayload.DraftID != "repo-copy" {
		t.Fatalf("workspace clone payload after confirm = %+v", gotPayload)
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	inputs := workspaceCloneFormInputs(t, cardData)
	if got, _ := inputs["repo_url"]["default_value"].(string); got != "git@github.com:example/repo.git" {
		t.Fatalf("repo_url default_value = %q, want git@github.com:example/repo.git", got)
	}
	if got, _ := inputs["workspace_id"]["default_value"].(string); got != "repo-copy" {
		t.Fatalf("workspace_id default_value = %q, want repo-copy", got)
	}
	buttons := workspaceCloneFormButtons(t, cardData)
	if got, _ := buttons["workspace_clone_pickdir"]["form_action_type"].(string); got != "submit" {
		t.Fatalf("workspace_clone_pickdir form_action_type = %q, want submit", got)
	}
	if got, _ := buttons["workspace_clone_submit"]["form_action_type"].(string); got != "submit" {
		t.Fatalf("workspace_clone_submit form_action_type = %q, want submit", got)
	}
	body := cardMarkdownContent(t, cardData)
	if !strings.Contains(body, parentDir) {
		t.Fatalf("workspace clone card body = %q, want parent dir %q", body, parentDir)
	}

	var submitResp *callback.CardActionTriggerResponse
	var submitErr error
	done := make(chan struct{})
	go func() {
		submitResp, submitErr = a.completeWorkspaceCloneSubmit(&feishu.CardAction{
			UserID:      "user-1",
			ChatID:      "chat-1",
			MessageID:   "msg-1",
			ActionValue: map[string]any{"request_id": "workspace-clone-1"},
			FormValue: map[string]any{
				"repo_url":     "git@github.com:example/repo.git",
				"workspace_id": "repo-copy",
			},
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("completeWorkspaceCloneSubmit() blocked on clone")
	}
	if submitErr != nil || submitResp == nil || submitResp.Toast == nil || submitResp.Toast.Type != "info" {
		t.Fatalf("completeWorkspaceCloneSubmit() = %#v, %v", submitResp, submitErr)
	}
	cardData, _ = submitResp.Card.Data.(map[string]any)
	body = cardMarkdownContent(t, cardData)
	if !strings.Contains(body, "正在从仓库创建工作区") || !strings.Contains(body, "这张卡片会自动刷新") {
		t.Fatalf("clone preparing card body = %q", body)
	}
	if pending := a.store.PendingByID("workspace-clone-1"); pending == nil || pending.Status != "processing" {
		t.Fatalf("pending after async submit = %+v, want processing", pending)
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patchedCards before clone completes = %+v, want none", ff.patchedCards)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("workspace clone did not start in background")
	}
	close(release)
	released = true

	wantTargetDir := filepath.Join(parentDir, "repo-copy")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(ff.patchedCards) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if gotRepoURL != "git@github.com:example/repo.git" {
		t.Fatalf("workspaceGitClone repoURL = %q", gotRepoURL)
	}
	if gotTargetDir != wantTargetDir {
		t.Fatalf("workspaceGitClone targetDir = %q, want %q", gotTargetDir, wantTargetDir)
	}
	if len(ff.patchedCards) == 0 {
		t.Fatalf("patchedCards after clone completes = %+v, want success card", ff.patchedCards)
	}
	if pending := a.store.PendingByID("workspace-clone-1"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after clone submit = %+v, want resolved", pending)
	}
	if ws := config.FindWorkspace(a.cfg, "repo-copy"); ws == nil || filepath.Clean(ws.Cwd) != filepath.Clean(wantTargetDir) {
		t.Fatalf("created workspace = %+v, want cwd %q", ws, wantTargetDir)
	}
	body = cardMarkdownContent(t, ff.patchedCards[len(ff.patchedCards)-1])
	if !strings.Contains(body, "已从仓库创建并切换到工作区 `repo-copy`") || !strings.Contains(body, wantTargetDir) {
		t.Fatalf("clone status card body = %q", body)
	}
}

func TestWorkspaceCloneSubmitExistingDirectoryTurnsIntoWorkspaceNew(t *testing.T) {
	a, _, _ := newTestApp(t)
	baseDir := t.TempDir()
	currentDir := filepath.Join(baseDir, "current")
	existingDir := filepath.Join(baseDir, "repo")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(currentDir) error = %v", err)
	}
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(existingDir) error = %v", err)
	}
	a.cfg.Workspaces[0].Cwd = currentDir

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "workspace-clone-existing",
		Kind:        "workspace_clone",
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(workspaceClonePayload{
			RootPath:          "/",
			SelectedParentDir: baseDir,
			RepoURL:           "git@github.com:example/repo.git",
		}),
	}); err != nil {
		t.Fatalf("UpsertPending(workspace-clone-existing) error = %v", err)
	}

	resp, err := a.completeWorkspaceCloneSubmit(&feishu.CardAction{
		UserID:      "user-1",
		ChatID:      "chat-1",
		ActionValue: map[string]any{"request_id": "workspace-clone-existing"},
		FormValue:   map[string]any{"repo_url": "git@github.com:example/repo.git"},
	})
	if err != nil || resp == nil || resp.Card == nil || resp.Toast == nil {
		t.Fatalf("completeWorkspaceCloneSubmit() = %#v, %v", resp, err)
	}
	if resp.Toast.Type != "info" || !strings.Contains(resp.Toast.Content, "预填好的新建工作区") {
		t.Fatalf("clone existing dir toast = %#v, want takeover hint", resp.Toast)
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	inputs := workspaceNewFormInputs(t, cardData)
	if got, _ := inputs["workspace_id"]["default_value"].(string); got != "repo" {
		t.Fatalf("workspace_id default_value = %q, want repo", got)
	}
	if body := cardMarkdownContent(t, cardData); !strings.Contains(body, existingDir) {
		t.Fatalf("workspace new takeover body = %q, want target dir %q", body, existingDir)
	}
	if pending := a.store.PendingByID("workspace-clone-existing"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("clone pending after takeover = %+v, want resolved", pending)
	}

	foundNewPending := false
	for _, req := range a.store.AllPendingRequests() {
		if req != nil && req.Kind == "workspace_new" {
			payload := workspaceNewPayloadFromPending(req)
			if filepath.Clean(payload.SelectedCWD) == filepath.Clean(existingDir) && payload.DraftID == "repo" {
				foundNewPending = true
				break
			}
		}
	}
	if !foundNewPending {
		t.Fatal("expected a new workspace_new pending request for takeover")
	}
}

func TestWorkspaceCloneSubmitExistingWorkspacePromptsSwitch(t *testing.T) {
	a, _, _ := newTestApp(t)
	baseDir := t.TempDir()
	existingDir := filepath.Join(baseDir, "repo")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(existingDir) error = %v", err)
	}
	a.cfg.Workspaces = append(a.cfg.Workspaces, config.Workspace{ID: "repo", Cwd: existingDir})

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "workspace-clone-existing-workspace",
		Kind:        "workspace_clone",
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		Status:      "pending",
		PayloadJSON: mustJSON(workspaceClonePayload{
			RootPath:          "/",
			SelectedParentDir: baseDir,
			RepoURL:           "git@github.com:example/repo.git",
		}),
	}); err != nil {
		t.Fatalf("UpsertPending(workspace-clone-existing-workspace) error = %v", err)
	}

	resp, err := a.completeWorkspaceCloneSubmit(&feishu.CardAction{
		UserID:      "user-1",
		ChatID:      "chat-1",
		ActionValue: map[string]any{"request_id": "workspace-clone-existing-workspace"},
		FormValue:   map[string]any{"repo_url": "git@github.com:example/repo.git"},
	})
	if err != nil || resp == nil || resp.Card == nil || resp.Toast == nil {
		t.Fatalf("completeWorkspaceCloneSubmit() = %#v, %v", resp, err)
	}
	if resp.Toast.Type != "info" || !strings.Contains(resp.Toast.Content, "现有工作区") {
		t.Fatalf("clone existing workspace toast = %#v, want switch hint", resp.Toast)
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	if body := cardMarkdownContent(t, cardData); !strings.Contains(body, existingDir) || !strings.Contains(body, "是否直接切换到这个工作区") {
		t.Fatalf("workspace existing switch body = %q", body)
	}
	if !cardHasButtonText(cardData, "切换到该工作区") {
		t.Fatalf("workspace existing switch card buttons = %#v", cardButtonsForTest(cardData))
	}
	if pending := a.store.PendingByID("workspace-clone-existing-workspace"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("clone pending after existing workspace hint = %+v, want resolved", pending)
	}
	for _, req := range a.store.AllPendingRequests() {
		if req != nil && req.Kind == "workspace_new" {
			t.Fatalf("did not expect workspace_new pending request, got %+v", req)
		}
	}
}

func TestWorkspaceCloneSubmitFailurePatchesRetryForm(t *testing.T) {
	a, ff, _ := newTestApp(t)
	baseDir := t.TempDir()
	parentDir := filepath.Join(baseDir, "parents")
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(parentDir) error = %v", err)
	}

	origClone := workspaceGitClone
	defer func() { workspaceGitClone = origClone }()

	workspaceGitClone = func(_ context.Context, _, _ string, _ workspaceCloneProgressReporter) error {
		return context.DeadlineExceeded
	}

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "workspace-clone-fail",
		Kind:        "workspace_clone",
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		FeishuMsgID: "msg-1",
		Status:      "pending",
		PayloadJSON: mustJSON(workspaceClonePayload{
			RootPath:          "/",
			SelectedParentDir: parentDir,
		}),
	}); err != nil {
		t.Fatalf("UpsertPending(workspace-clone-fail) error = %v", err)
	}

	resp, err := a.completeWorkspaceCloneSubmit(&feishu.CardAction{
		UserID:      "user-1",
		ChatID:      "chat-1",
		MessageID:   "msg-1",
		ActionValue: map[string]any{"request_id": "workspace-clone-fail"},
		FormValue: map[string]any{
			"repo_url":     "git@github.com:example/repo.git",
			"workspace_id": "repo-copy",
		},
	})
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "info" {
		t.Fatalf("completeWorkspaceCloneSubmit() = %#v, %v", resp, err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(ff.patchedCards) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(ff.patchedCards) == 0 {
		t.Fatalf("patchedCards after clone failure = %+v, want retry form", ff.patchedCards)
	}
	if pending := a.store.PendingByID("workspace-clone-fail"); pending == nil || pending.Status != "pending" {
		t.Fatalf("pending after clone failure = %+v, want pending", pending)
	}
	body := cardMarkdownContent(t, ff.patchedCards[len(ff.patchedCards)-1])
	if !strings.Contains(body, "最近一次创建失败") || !strings.Contains(body, context.DeadlineExceeded.Error()) {
		t.Fatalf("clone failure card body = %q", body)
	}
	inputs := workspaceCloneFormInputs(t, ff.patchedCards[len(ff.patchedCards)-1])
	if got, _ := inputs["repo_url"]["default_value"].(string); got != "git@github.com:example/repo.git" {
		t.Fatalf("repo_url default_value after failure = %q", got)
	}
	if got, _ := inputs["workspace_id"]["default_value"].(string); got != "repo-copy" {
		t.Fatalf("workspace_id default_value after failure = %q", got)
	}
}

func TestWorkspaceCloneSubmitCreateWorkspaceFailurePatchesManualHint(t *testing.T) {
	a, ff, _ := newTestApp(t)
	baseDir := t.TempDir()
	parentDir := filepath.Join(baseDir, "parents")
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(parentDir) error = %v", err)
	}
	a.cfg.Log.Level = "trace"

	origClone := workspaceGitClone
	defer func() { workspaceGitClone = origClone }()

	workspaceGitClone = func(_ context.Context, _, targetDir string, _ workspaceCloneProgressReporter) error {
		return os.MkdirAll(filepath.Join(targetDir, ".git"), 0o755)
	}

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "workspace-clone-manual",
		Kind:        "workspace_clone",
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		FeishuMsgID: "msg-1",
		Status:      "pending",
		PayloadJSON: mustJSON(workspaceClonePayload{
			RootPath:          "/",
			SelectedParentDir: parentDir,
		}),
	}); err != nil {
		t.Fatalf("UpsertPending(workspace-clone-manual) error = %v", err)
	}

	resp, err := a.completeWorkspaceCloneSubmit(&feishu.CardAction{
		UserID:      "user-1",
		ChatID:      "chat-1",
		MessageID:   "msg-1",
		ActionValue: map[string]any{"request_id": "workspace-clone-manual"},
		FormValue: map[string]any{
			"repo_url":     "git@github.com:example/repo.git",
			"workspace_id": "repo-copy",
		},
	})
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "info" {
		t.Fatalf("completeWorkspaceCloneSubmit() = %#v, %v", resp, err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(ff.patchedCards) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(ff.patchedCards) == 0 {
		t.Fatalf("patchedCards after manual hint = %+v, want status card", ff.patchedCards)
	}
	if pending := a.store.PendingByID("workspace-clone-manual"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after create workspace failure = %+v, want resolved", pending)
	}
	body := cardMarkdownContent(t, ff.patchedCards[len(ff.patchedCards)-1])
	wantTargetDir := filepath.Join(parentDir, "repo-copy")
	if !strings.Contains(body, "仓库已拉取，可手动接管") || !strings.Contains(body, wantTargetDir) {
		t.Fatalf("manual hint body = %q", body)
	}
	if !strings.Contains(body, "/workspace new") || !strings.Contains(body, "unsupported log.level") {
		t.Fatalf("manual hint body = %q, want workspace new guidance and underlying error", body)
	}
	if cardHasButtonText(ff.patchedCards[len(ff.patchedCards)-1], "接管为工作区") {
		t.Fatalf("manual hint card should not include takeover button: %#v", ff.patchedCards[len(ff.patchedCards)-1])
	}
	if ws := config.FindWorkspace(a.cfg, "repo-copy"); ws != nil {
		t.Fatalf("workspace should not be registered on manual hint path: %+v", ws)
	}
}

func TestWorkspaceCloneSubmitPatchesProgressAndSupportsCancel(t *testing.T) {
	a, ff, _ := newTestApp(t)
	baseDir := t.TempDir()
	parentDir := filepath.Join(baseDir, "parents")
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(parentDir) error = %v", err)
	}

	origClone := workspaceGitClone
	defer func() { workspaceGitClone = origClone }()

	started := make(chan struct{})
	workspaceGitClone = func(ctx context.Context, _, targetDir string, report workspaceCloneProgressReporter) error {
		if report != nil {
			report("Cloning into '" + filepath.Base(targetDir) + "'...")
			close(started)
			time.Sleep(workspaceClonePatchInterval + 20*time.Millisecond)
			report("Receiving objects: 42% (42/100)")
		}
		<-ctx.Done()
		return ctx.Err()
	}

	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "workspace-clone-cancel",
		Kind:        "workspace_clone",
		SessionKey:  "sess-1",
		OwnerUserID: "user-1",
		FeishuMsgID: "msg-1",
		Status:      "pending",
		PayloadJSON: mustJSON(workspaceClonePayload{
			RootPath:          "/",
			SelectedParentDir: parentDir,
		}),
	}); err != nil {
		t.Fatalf("UpsertPending(workspace-clone-cancel) error = %v", err)
	}

	resp, err := a.completeWorkspaceCloneSubmit(&feishu.CardAction{
		UserID:      "user-1",
		ChatID:      "chat-1",
		MessageID:   "msg-1",
		ActionValue: map[string]any{"request_id": "workspace-clone-cancel"},
		FormValue: map[string]any{
			"repo_url":     "git@github.com:example/repo.git",
			"workspace_id": "repo-copy",
		},
	})
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Type != "info" {
		t.Fatalf("completeWorkspaceCloneSubmit() = %#v, %v", resp, err)
	}
	if !cardHasButtonText(resp.Card.Data.(map[string]any), "取消克隆") {
		t.Fatalf("preparing card missing cancel button: %#v", resp.Card.Data)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("workspace clone did not start")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(ff.patchedCards) > 0 && strings.Contains(cardMarkdownContent(t, ff.patchedCards[len(ff.patchedCards)-1]), "Receiving objects: 42%") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(ff.patchedCards) == 0 {
		t.Fatalf("patchedCards after progress = %+v, want progress card", ff.patchedCards)
	}
	progressBody := cardMarkdownContent(t, ff.patchedCards[len(ff.patchedCards)-1])
	if !strings.Contains(progressBody, "Receiving objects: 42%") {
		t.Fatalf("progress body = %q, want streamed git progress", progressBody)
	}

	cancelResp, err := a.completeWorkspaceCloneCancel(&feishu.CardAction{
		UserID:      "user-1",
		ChatID:      "chat-1",
		MessageID:   "msg-1",
		ActionValue: map[string]any{"request_id": "workspace-clone-cancel"},
	})
	if err != nil || cancelResp == nil || cancelResp.Toast == nil || cancelResp.Toast.Type != "info" {
		t.Fatalf("completeWorkspaceCloneCancel() = %#v, %v", cancelResp, err)
	}
	cancelBody := cardMarkdownContent(t, cancelResp.Card.Data.(map[string]any))
	if !strings.Contains(cancelBody, "正在取消仓库克隆") {
		t.Fatalf("cancel response body = %q", cancelBody)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pending := a.store.PendingByID("workspace-clone-cancel"); pending != nil && pending.Status == "resolved" && len(ff.patchedCards) > 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pending := a.store.PendingByID("workspace-clone-cancel"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after cancel = %+v, want resolved", pending)
	}
	finalBody := cardMarkdownContent(t, ff.patchedCards[len(ff.patchedCards)-1])
	if !strings.Contains(finalBody, "已取消仓库克隆") {
		t.Fatalf("final cancel body = %q", finalBody)
	}
}

func TestDownloadFilePickAndConfirmSharesFile(t *testing.T) {
	a, ff, _ := newTestApp(t)
	root := a.cfg.Workspaces[0].Cwd
	target := filepath.Join(root, "report.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(report.txt) error = %v", err)
	}
	ff.sharedFileResult = feishu.SharedFileResult{
		FileName:  "report.txt",
		URL:       "https://drive.example/file-1",
		SizeBytes: 5,
	}

	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat-1", ChatType: "group", UserID: "user-1"}
	if err := a.commandDownload(msg, nil); err != nil {
		t.Fatalf("commandDownload() error = %v", err)
	}
	pending := a.store.AllPendingRequests()
	if len(pending) != 1 || pending[0].Kind != downloadFilePendingKind {
		t.Fatalf("download pending requests = %+v", pending)
	}
	requestID := pending[0].ID

	resp, err := a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		ChatID:      "chat-1",
		ActionValue: map[string]any{"request_id": requestID},
		Option:      encodePathPickerOption(pathPickerEntry{Name: "report.txt", Path: target, IsDir: false}),
	}, "path_picker.dropdown")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("download picker dropdown = %#v, %v", resp, err)
	}
	resp, err = a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		ChatID:      "chat-1",
		ActionValue: map[string]any{"request_id": requestID},
	}, "path_picker.confirm")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("download picker confirm = %#v, %v", resp, err)
	}
	cardData, _ := resp.Card.Data.(map[string]any)
	body := cardMarkdownContent(t, cardData)
	if !strings.Contains(body, "正在生成文件下载链接") || !strings.Contains(body, "report.txt") {
		t.Fatalf("download preparing card body = %q", body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(ff.patchedCards) > 0 && len(ff.sharedFileRequests) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(ff.sharedFileRequests) != 1 {
		t.Fatalf("sharedFileRequests = %+v, want 1", ff.sharedFileRequests)
	}
	if got := filepath.Clean(ff.sharedFileRequests[0].LocalPath); got != filepath.Clean(target) {
		t.Fatalf("share local path = %q, want %q", got, target)
	}
	if got := ff.sharedFileRequests[0].ChatID; got != "chat-1" {
		t.Fatalf("share chat id = %q, want chat-1", got)
	}
	if len(ff.patchedCards) == 0 {
		t.Fatalf("patchedCards = %+v, want final download card", ff.patchedCards)
	}
	finalBody := cardMarkdownContent(t, ff.patchedCards[len(ff.patchedCards)-1])
	if !strings.Contains(finalBody, "https://drive.example/file-1") || !strings.Contains(finalBody, "report.txt") {
		t.Fatalf("download result card body = %q", finalBody)
	}
	if got := a.store.PendingByID(requestID); got == nil || got.Status != "resolved" {
		t.Fatalf("download pending after confirm = %+v", got)
	}
}

func TestPathPickerUpgradeLocalBinaryConfirmStagesArtifact(t *testing.T) {
	origManager := newDaemonManager
	origVersion := currentVersion
	origGOARCH := currentGOARCH
	defer func() {
		newDaemonManager = origManager
		currentVersion = origVersion
		currentGOARCH = origGOARCH
	}()

	a, _, _ := newTestApp(t)
	newDaemonManager = func(string) (daemon.Manager, error) {
		return &fakeDaemonManagerForApp{status: &daemon.Status{Installed: true, Running: true, PID: os.Getpid()}}, nil
	}
	currentVersion = func() string { return "v0.1.0" }
	currentGOARCH = func() string { return "amd64" }
	sessionKey := "sess-upgrade"
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: "default",
		OwnerUserID: "user-1",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	sourcePath := filepath.Join(a.cfg.Workspaces[0].Cwd, "dist", "feidex-linux-amd64")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(source) error = %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("upgrade-local"), 0o755); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          "upgrade-local-picker",
		Kind:        upgradeLocalBinaryPendingKind,
		SessionKey:  sessionKey,
		OwnerUserID: "user-1",
		FeishuMsgID: "msg-1",
		Status:      "pending",
		PayloadJSON: mustJSON(pathPickerPayload{
			Mode:        pathPickerModeFile,
			Style:       pathPickerStyleDropdown,
			RootPath:    a.cfg.Workspaces[0].Cwd,
			CurrentPath: a.cfg.Workspaces[0].Cwd,
		}),
	}); err != nil {
		t.Fatalf("UpsertPending(upgrade local picker) error = %v", err)
	}

	resp, err := a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		MessageID:   "msg-1",
		ActionValue: map[string]any{"request_id": "upgrade-local-picker"},
		Option:      encodePathPickerOption(pathPickerEntry{Name: filepath.Base(sourcePath), Path: sourcePath, IsDir: false}),
	}, "path_picker.dropdown")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("upgrade local dropdown = %#v, %v", resp, err)
	}
	resp, err = a.completePathPickerAction(&feishu.CardAction{
		UserID:      "user-1",
		MessageID:   "msg-1",
		ActionValue: map[string]any{"request_id": "upgrade-local-picker"},
	}, "path_picker.confirm")
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("upgrade local confirm = %#v, %v", resp, err)
	}
	if pending := a.store.PendingByID("upgrade-local-picker"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("upgrade local picker pending = %+v, want resolved", pending)
	}
	found := false
	for _, req := range a.store.AllPendingRequests() {
		if req.Kind != "upgrade_release" {
			continue
		}
		var payload upgradePendingPayload
		if err := json.Unmarshal([]byte(req.PayloadJSON), &payload); err != nil {
			t.Fatalf("Unmarshal(upgrade local payload) error = %v", err)
		}
		if payload.SourcePath == "" {
			continue
		}
		content, err := os.ReadFile(payload.SourcePath)
		if err != nil {
			t.Fatalf("ReadFile(staged) error = %v", err)
		}
		if string(content) != "upgrade-local" {
			t.Fatalf("staged content = %q, want upgrade-local", string(content))
		}
		found = true
		break
	}
	if !found {
		t.Fatal("expected staged local upgrade request")
	}
}

func cardHasTag(card map[string]any, wantTag string) bool {
	elements := cardElements(card)
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag == wantTag {
			return true
		}
		actions, _ := elem["actions"].([]map[string]any)
		for _, action := range actions {
			if tag, _ := action["tag"].(string); tag == wantTag {
				return true
			}
		}
		columns, _ := elem["columns"].([]map[string]any)
		for _, column := range columns {
			columnElems, _ := column["elements"].([]map[string]any)
			for _, child := range columnElems {
				if tag, _ := child["tag"].(string); tag == wantTag {
					return true
				}
			}
		}
	}
	return false
}

func cardHasButtonText(card map[string]any, want string) bool {
	for _, button := range cardButtonsForTest(card) {
		text, _ := button["text"].(map[string]any)
		content, _ := text["content"].(string)
		if content == want {
			return true
		}
	}
	return false
}

func cardElements(card map[string]any) []map[string]any {
	if elements, ok := card["elements"].([]map[string]any); ok {
		return elements
	}
	body, _ := card["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	return elements
}

func workspaceNewFormInputs(t *testing.T, card map[string]any) map[string]map[string]any {
	t.Helper()
	form := workspaceNewForm(t, card)
	elements, _ := form["elements"].([]map[string]any)
	inputs := make(map[string]map[string]any)
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag != "input" {
			continue
		}
		name, _ := elem["name"].(string)
		inputs[name] = elem
	}
	return inputs
}

func workspaceNewFormButtons(t *testing.T, card map[string]any) map[string]map[string]any {
	t.Helper()
	form := workspaceNewForm(t, card)
	elements, _ := form["elements"].([]map[string]any)
	buttons := make(map[string]map[string]any)
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag != "column_set" {
			continue
		}
		columns, _ := elem["columns"].([]map[string]any)
		for _, column := range columns {
			columnElems, _ := column["elements"].([]map[string]any)
			for _, child := range columnElems {
				if tag, _ := child["tag"].(string); tag != "button" {
					continue
				}
				name, _ := child["name"].(string)
				buttons[name] = child
			}
		}
	}
	return buttons
}

func workspaceNewForm(t *testing.T, card map[string]any) map[string]any {
	t.Helper()
	for _, elem := range cardElements(card) {
		if tag, _ := elem["tag"].(string); tag == "form" {
			return elem
		}
	}
	t.Fatalf("workspace new card missing form: %#v", card)
	return nil
}

func workspaceCloneFormInputs(t *testing.T, card map[string]any) map[string]map[string]any {
	t.Helper()
	form := workspaceCloneForm(t, card)
	elements, _ := form["elements"].([]map[string]any)
	inputs := make(map[string]map[string]any)
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag != "input" {
			continue
		}
		name, _ := elem["name"].(string)
		inputs[name] = elem
	}
	return inputs
}

func workspaceCloneFormButtons(t *testing.T, card map[string]any) map[string]map[string]any {
	t.Helper()
	form := workspaceCloneForm(t, card)
	elements, _ := form["elements"].([]map[string]any)
	buttons := make(map[string]map[string]any)
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag != "column_set" {
			continue
		}
		columns, _ := elem["columns"].([]map[string]any)
		for _, column := range columns {
			columnElems, _ := column["elements"].([]map[string]any)
			for _, child := range columnElems {
				if tag, _ := child["tag"].(string); tag != "button" {
					continue
				}
				name, _ := child["name"].(string)
				buttons[name] = child
			}
		}
	}
	return buttons
}

func workspaceCloneForm(t *testing.T, card map[string]any) map[string]any {
	t.Helper()
	for _, elem := range cardElements(card) {
		if tag, _ := elem["tag"].(string); tag == "form" {
			name, _ := elem["name"].(string)
			if name == "workspace_clone_form" {
				return elem
			}
		}
	}
	t.Fatalf("workspace clone card missing form: %#v", card)
	return nil
}
