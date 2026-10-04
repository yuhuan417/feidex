package feishuapp

import (
	"errors"
	appdebugviewcmd "feidex/internal/adapter/feishu/debugviewcmd"
	"feidex/internal/application/fileshare"
	"feidex/internal/domain/conversation"
	"path/filepath"
	"strings"
	"testing"

	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestDownloadHelpersAndFileShareBranches(t *testing.T) {
	a, ff, _ := newTestApp(t)
	workspace := a.cfg.Workspaces[0].Cwd
	selectedPath := filepath.Join(workspace, "report.txt")

	payload, err := appdebugviewcmd.NewDownloadPathPickerPayload(&a.cfg.Workspaces[0])
	if err != nil {
		t.Fatalf("appdebugviewcmd.NewDownloadPathPickerPayload() error = %v", err)
	}
	if payload.RootPath != workspace || payload.CurrentPath != workspace || payload.Mode != appworkspacecmd.PathPickerModeFile || payload.Style != appworkspacecmd.PathPickerStyleDropdown {
		t.Fatalf("appdebugviewcmd.NewDownloadPathPickerPayload() = %+v", payload)
	}

	if got := appdebugviewcmd.RenderDownloadDisplayPath(selectedPath, workspace); got != "report.txt" {
		t.Fatalf("appdebugviewcmd.RenderDownloadDisplayPath(internal) = %q", got)
	}
	if got := appdebugviewcmd.RenderDownloadDisplayPath(filepath.Join(workspace, "..", "outside.txt"), workspace); !filepath.IsAbs(got) {
		t.Fatalf("appdebugviewcmd.RenderDownloadDisplayPath(external) = %q", got)
	}
	if got := appdebugviewcmd.RenderDownloadDisplayPath("", workspace); got != "-" {
		t.Fatalf("appdebugviewcmd.RenderDownloadDisplayPath(empty) = %q", got)
	}

	for size, want := range map[int64]string{
		512:             "512 B",
		2048:            "2.0 KB",
		5 * 1024 * 1024: "5.00 MB",
	} {
		if got := appdebugviewcmd.FormatDownloadSize(size); got != want {
			t.Fatalf("appdebugviewcmd.FormatDownloadSize(%d) = %q, want %q", size, got, want)
		}
	}

	if err := a.store.UpsertPending(&state.PendingRequest{ID: "download-ok", Kind: fileshare.Kind, Status: "processing"}); err != nil {
		t.Fatalf("UpsertPending(download-ok) error = %v", err)
	}
	ff.sharedFileResult = feishu.SharedFileResult{
		FileName:  "report.txt",
		URL:       "https://example.test/file",
		SizeBytes: 2048,
	}
	a.bindings.FileSharing.Execute(fileshare.Execution{ID: "download-ok", MessageID: "msg-ok", Payload: payload, WorkspaceCWD: workspace, Request: fileshare.Request{LocalPath: selectedPath, ChatID: "chat-1", UserID: "user-1"}})
	if req := a.store.PendingByID("download-ok"); req == nil || req.Status != "resolved" {
		t.Fatalf("pending after success = %+v", req)
	}
	if len(ff.patchedCards) == 0 {
		t.Fatal("finishDownloadFileShare(success) should patch card")
	}
	if body := cardMarkdownContent(t, ff.patchedCards[len(ff.patchedCards)-1]); !strings.Contains(body, "已生成文件下载链接") || !strings.Contains(body, "2.0 KB") {
		t.Fatalf("success patched body = %q", body)
	}

	if err := a.store.UpsertPending(&state.PendingRequest{ID: "download-fail", Kind: fileshare.Kind, Status: "processing"}); err != nil {
		t.Fatalf("UpsertPending(download-fail) error = %v", err)
	}
	ff.shareFileErr = errors.New("share boom")
	before := len(ff.patchedCards)
	a.bindings.FileSharing.Execute(fileshare.Execution{ID: "download-fail", MessageID: "msg-fail", Payload: payload, WorkspaceCWD: workspace, Request: fileshare.Request{LocalPath: selectedPath, ChatID: "chat-1", UserID: "user-1"}})
	if req := a.store.PendingByID("download-fail"); req == nil || req.Status != "pending" {
		t.Fatalf("pending after failure = %+v", req)
	}
	if len(ff.patchedCards) != before+1 {
		t.Fatalf("patchedCards after failure = %d, want %d", len(ff.patchedCards), before+1)
	}

	ff.shareFileErr = nil
	before = len(ff.patchedCards)
	a.bindings.FileSharing.Execute(fileshare.Execution{ID: "download-ok", Payload: payload, WorkspaceCWD: workspace, Request: fileshare.Request{LocalPath: selectedPath, ChatID: "chat-1", UserID: "user-1"}})
	if len(ff.patchedCards) != before {
		t.Fatalf("finishDownloadFileShare(empty message id) patchedCards = %d, want %d", len(ff.patchedCards), before)
	}
}

func TestCompleteDownloadFileConfirmBranches(t *testing.T) {
	a, ff, _ := newTestApp(t)
	workspace := a.cfg.Workspaces[0].Cwd
	selectedPath := filepath.Join(workspace, "report.txt")
	payload := appworkspacecmd.PathPickerPayload{
		Mode:        appworkspacecmd.PathPickerModeFile,
		Style:       appworkspacecmd.PathPickerStyleDropdown,
		RootPath:    workspace,
		CurrentPath: workspace,
	}

	resp, err := appdebugviewcmd.CompleteDownloadFileConfirm(DebugViewDependencies(a), &feishu.CardAction{}, nil, payload, selectedPath)
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Content != "下载请求已过期" {
		t.Fatalf("completeDownloadFileConfirm(nil pending) = %+v, %v", resp, err)
	}

	resp, err = appdebugviewcmd.CompleteDownloadFileConfirm(DebugViewDependencies(a), &feishu.CardAction{}, &state.PendingRequest{Status: "processing"}, payload, selectedPath)
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Content != "正在生成下载链接，请稍候" {
		t.Fatalf("completeDownloadFileConfirm(processing) = %+v, %v", resp, err)
	}

	if err := a.store.UpsertSession(&conversation.Session{
		Key:         "sess-download",
		WorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:      "chat-session",
	}); err != nil {
		t.Fatalf("UpsertSession(sess-download) error = %v", err)
	}
	pending := &state.PendingRequest{
		ID:          "download-confirm",
		Kind:        fileshare.Kind,
		SessionKey:  "sess-download",
		OwnerUserID: "owner-1",
		FeishuMsgID: "pending-msg",
		Status:      "pending",
	}
	if err := a.store.UpsertPending(pending); err != nil {
		t.Fatalf("UpsertPending(download-confirm) error = %v", err)
	}
	ff.sharedFileResult = feishu.SharedFileResult{FileName: "report.txt", URL: "https://example.test/download"}
	resp, err = appdebugviewcmd.CompleteDownloadFileConfirm(DebugViewDependencies(a), &feishu.CardAction{
		ChatID:    "",
		UserID:    "owner-1",
		MessageID: "",
	}, pending, payload, selectedPath)
	if err != nil || resp == nil || resp.Toast == nil || resp.Toast.Content != "正在生成下载链接" {
		t.Fatalf("completeDownloadFileConfirm(start) = %+v, %v", resp, err)
	}
	a.waitAsync()
	if req := a.store.PendingByID("download-confirm"); req == nil || req.Status == "" {
		t.Fatalf("pending after completeDownloadFileConfirm(start) = %+v", req)
	}
	if len(ff.sharedFileRequests) == 0 {
		t.Fatal("completeDownloadFileConfirm(start) should trigger ShareLocalFile")
	}
	lastReq := ff.sharedFileRequests[len(ff.sharedFileRequests)-1]
	if lastReq.ChatID != "chat-session" || lastReq.UserID != "owner-1" || lastReq.LocalPath != selectedPath {
		t.Fatalf("sharedFileRequest = %+v", lastReq)
	}
}
