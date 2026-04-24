package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestAppStateFacadeBranches(t *testing.T) {
	var nilFacade *appStateFacade
	if got := nilFacade.session("sess"); got != nil {
		t.Fatalf("nil session() = %+v, want nil", got)
	}
	if got := nilFacade.sessions(); got != nil {
		t.Fatalf("nil sessions() = %+v, want nil", got)
	}
	if err := nilFacade.saveSession(&state.Session{}); err != nil {
		t.Fatalf("nil saveSession() error = %v", err)
	}
	if id, err := nilFacade.createSubmission(&state.Submission{}); id != "" || err != nil {
		t.Fatalf("nil createSubmission() = %q, %v", id, err)
	}
	nilFacade.deleteSubmission("sub")
	if got := nilFacade.submission("sub"); got != nil {
		t.Fatalf("nil submission() = %+v, want nil", got)
	}
	if err := nilFacade.updateSubmission("sub", func(*state.Submission) {}); err != nil {
		t.Fatalf("nil updateSubmission() error = %v", err)
	}
	if err := nilFacade.queueSubmission("sess", "sub"); err != nil {
		t.Fatalf("nil queueSubmission() error = %v", err)
	}
	if got, err := nilFacade.dequeueSubmission("sess"); got != "" || err != nil {
		t.Fatalf("nil dequeueSubmission() = %q, %v", got, err)
	}
	if got := nilFacade.pending("req"); got != nil {
		t.Fatalf("nil pending() = %+v, want nil", got)
	}
	if err := nilFacade.savePending(&state.PendingRequest{}); err != nil {
		t.Fatalf("nil savePending() error = %v", err)
	}
	if got := nilFacade.pendingRequests(); got != nil {
		t.Fatalf("nil pendingRequests() = %+v, want nil", got)
	}
	if err := nilFacade.updatePending("req", func(*state.PendingRequest) {}); err != nil {
		t.Fatalf("nil updatePending() error = %v", err)
	}
	nilFacade.deletePendingRequests(nil)
	if got, err := nilFacade.nextLocalID("x"); got != "" || err != nil {
		t.Fatalf("nil nextLocalID() = %q, %v", got, err)
	}
	if got := nilFacade.messageLink("msg"); got != nil {
		t.Fatalf("nil messageLink() = %+v, want nil", got)
	}
	if err := nilFacade.saveMessageLink(&state.MessageLink{}); err != nil {
		t.Fatalf("nil saveMessageLink() error = %v", err)
	}
	nilFacade.deleteMessageLinks(nil)

	a, _, _ := newTestApp(t)
	facade := a.appState()
	if facade == nil || facade.store != a.store {
		t.Fatalf("appState() = %+v", facade)
	}
	if err := facade.saveSession(&state.Session{Key: "sess-1", WorkspaceID: a.cfg.Workspaces[0].ID}); err != nil {
		t.Fatalf("saveSession() error = %v", err)
	}
	if got := facade.session(" sess-1 "); got == nil || got.Key != "sess-1" {
		t.Fatalf("session() = %+v", got)
	}
	if got := facade.sessions(); len(got) == 0 {
		t.Fatalf("sessions() = %+v", got)
	}

	subID, err := facade.createSubmission(&state.Submission{SessionKey: "sess-1", WorkspaceID: a.cfg.Workspaces[0].ID})
	if err != nil || subID == "" {
		t.Fatalf("createSubmission() = %q, %v", subID, err)
	}
	if got := facade.submission(subID); got == nil || got.ID != subID {
		t.Fatalf("submission() = %+v", got)
	}
	if err := facade.setSubmissionStatus(subID, "queued"); err != nil {
		t.Fatalf("setSubmissionStatus() error = %v", err)
	}
	if err := facade.markSubmissionRunning(subID, "thread-1", "turn-1"); err != nil {
		t.Fatalf("markSubmissionRunning() error = %v", err)
	}
	if err := facade.finalizeSubmission(subID, "done"); err != nil {
		t.Fatalf("finalizeSubmission() error = %v", err)
	}
	if got := facade.submission(subID); got == nil || got.Status != "done" || !got.Finalized || got.ThreadID != "thread-1" || got.TurnID != "turn-1" {
		t.Fatalf("submission after finalize = %+v", got)
	}
	facade.deleteSubmission(" " + subID + " ")
	if got := facade.submission(subID); got != nil {
		t.Fatalf("deleteSubmission() should remove, got %+v", got)
	}

	if err := facade.queueSubmission(" sess-1 ", " sub-2 "); err != nil {
		t.Fatalf("queueSubmission() error = %v", err)
	}
	if got, err := facade.dequeueSubmission("sess-1"); err != nil || got != "sub-2" {
		t.Fatalf("dequeueSubmission() = %q, %v", got, err)
	}

	if err := facade.savePending(&state.PendingRequest{ID: "req-1", Status: "pending"}); err != nil {
		t.Fatalf("savePending() error = %v", err)
	}
	if got := facade.pending(" req-1 "); got == nil || got.ID != "req-1" {
		t.Fatalf("pending() = %+v", got)
	}
	if got := facade.pendingRequests(); len(got) != 1 {
		t.Fatalf("pendingRequests() = %+v", got)
	}
	if err := facade.updatePending("req-1", func(req *state.PendingRequest) { req.Status = "done" }); err != nil {
		t.Fatalf("updatePending() error = %v", err)
	}
	if got := facade.resolvePending("req-1"); got == nil || got.Status != "resolved" {
		t.Fatalf("resolvePending() = %+v", got)
	}
	facade.deletePendingRequests(func(req *state.PendingRequest) bool { return req != nil && req.ID == "req-1" })
	if got := facade.pending("req-1"); got != nil {
		t.Fatalf("deletePendingRequests() should remove, got %+v", got)
	}

	localID, err := facade.nextLocalID(" x ")
	if err != nil || !strings.HasPrefix(localID, "x-") {
		t.Fatalf("nextLocalID() = %q, %v", localID, err)
	}

	if err := facade.saveMessageLink(&state.MessageLink{MessageID: "msg-1", TurnID: "turn-1"}); err != nil {
		t.Fatalf("saveMessageLink() error = %v", err)
	}
	if got := facade.messageLink(" msg-1 "); got == nil || got.TurnID != "turn-1" {
		t.Fatalf("messageLink() = %+v", got)
	}
	facade.deleteMessageLinks(func(link *state.MessageLink) bool { return link != nil && link.MessageID == "msg-1" })
	if got := facade.messageLink("msg-1"); got != nil {
		t.Fatalf("deleteMessageLinks() should remove, got %+v", got)
	}
}

func TestNotifyingFeishuClientWrapperDelegates(t *testing.T) {
	if got := wrapFeishuClient(nil); got != nil {
		t.Fatalf("wrapFeishuClient(nil) = %+v, want nil", got)
	}

	base := &fakeFeishuClient{
		rewriteLocalFileLinksOut: "preview",
		cleanupResult:            feishu.PreviewDriveCleanupResult{DeletedFileCount: 2},
		downloadPath:             "/tmp/file",
		downloadName:             "file.txt",
		sharedFileResult:         feishu.SharedFileResult{FileName: "file.txt", URL: "https://example.test/file"},
		mergeForwardText:         "merged",
		mergeForwardAttachments: []feishu.Attachment{
			{Kind: "file", ResourceKey: "fk-1"},
		},
	}
	client, _ := wrapFeishuClient(base).(*notifyingFeishuClient)
	if client == nil {
		t.Fatal("wrapFeishuClient() should return notifying client")
	}

	called := false
	client.SetHandlers(func(*feishu.InboundMessage) { called = true }, func(*feishu.CardAction) (*callback.CardActionTriggerResponse, error) { return nil, nil }, nil, nil, nil)
	if base.onMessage == nil {
		t.Fatal("SetHandlers() should delegate")
	}
	base.onMessage(&feishu.InboundMessage{})
	if !called {
		t.Fatal("delegated onMessage handler was not wired")
	}
	client.ConfigureLocalFileLinks("/tmp/state.json", "/repo")
	if base.localFileLinkStatePath != "/tmp/state.json" || base.localFileLinkProcessCWD != "/repo" {
		t.Fatalf("ConfigureLocalFileLinks() = %q %q", base.localFileLinkStatePath, base.localFileLinkProcessCWD)
	}
	if text, err := client.RewriteLocalFileLinks(context.Background(), feishu.LocalFileLinkRewriteRequest{Text: "x"}); err != nil || text != "preview" {
		t.Fatalf("RewriteLocalFileLinks() = %q, %v", text, err)
	}
	if result, err := client.CleanupArtifactsBefore(context.Background(), time.Now()); err != nil || result.DeletedFileCount != 2 {
		t.Fatalf("CleanupArtifactsBefore() = %+v, %v", result, err)
	}
	if err := client.ReplyText(context.Background(), "msg-1", "hello", true); err != nil || len(base.replyTexts) != 1 {
		t.Fatalf("ReplyText() error = %v, texts=%v", err, base.replyTexts)
	}
	if id, err := client.ReplyTextWithID(context.Background(), "msg-1", "hello-id", false); err != nil || id != "reply-text-id" {
		t.Fatalf("ReplyTextWithID() = %q, %v", id, err)
	}
	if err := client.SendText(context.Background(), "chat-1", "send"); err != nil || len(base.sentTexts) != 1 {
		t.Fatalf("SendText() error = %v, texts=%v", err, base.sentTexts)
	}
	if id, err := client.ReplyCard(context.Background(), "msg-1", map[string]any{"k": "v"}, false); err != nil || id != "reply-card-id" {
		t.Fatalf("ReplyCard() = %q, %v", id, err)
	}
	if id, err := client.SendCard(context.Background(), "chat-1", map[string]any{"k": "v"}); err != nil || id != "send-card-id" {
		t.Fatalf("SendCard() = %q, %v", id, err)
	}
	if err := client.PatchCard(context.Background(), "msg-1", map[string]any{"k": "v"}); err != nil || len(base.patchedCards) != 1 {
		t.Fatalf("PatchCard() error = %v, patched=%d", err, len(base.patchedCards))
	}
	if path, name, err := client.DownloadMessageResource(context.Background(), "msg-1", feishu.Attachment{Kind: "file"}, "/tmp"); err != nil || path != "/tmp/file" || name != "file.txt" {
		t.Fatalf("DownloadMessageResource() = %q %q %v", path, name, err)
	}
	if text, attachments, err := client.ResolveMergeForward(context.Background(), "msg-1", []string{"a", "b"}); err != nil || text != "merged" || len(attachments) != 1 {
		t.Fatalf("ResolveMergeForward() = %q %+v %v", text, attachments, err)
	}
	if result, err := client.ShareLocalFile(context.Background(), feishu.SharedFileRequest{LocalPath: "/tmp/file", ChatID: "chat-1"}); err != nil || result.FileName != "file.txt" {
		t.Fatalf("ShareLocalFile() = %+v, %v", result, err)
	}
	if card := client.SimpleStatusCard("title", "blue", "body", nil); card == nil {
		t.Fatal("SimpleStatusCard() should delegate")
	}
	if key := client.permissionIssueKey(feishuNotifyTarget{ChatID: "chat-1", MessageID: "msg-1"}, &feishu.PermissionIssue{API: "im.message.create", Code: 1, Message: "denied", LogID: "log-1"}); !strings.Contains(key, "chat-1|msg-1|im.message.create|1|denied|log-1") {
		t.Fatalf("permissionIssueKey() = %q", key)
	}
}

func TestCommandCaptureClientWrapperDelegates(t *testing.T) {
	base := &fakeFeishuClient{
		startErr:                 context.Canceled,
		rewriteLocalFileLinksOut: "preview",
		cleanupResult:            feishu.PreviewDriveCleanupResult{DeletedFileCount: 1},
		downloadPath:             "/tmp/file",
		downloadName:             "name.txt",
		sharedFileResult:         feishu.SharedFileResult{FileName: "name.txt", URL: "https://example.test/file"},
		mergeForwardText:         "merged",
	}
	capture := &commandCaptureClient{base: base, replyMessageID: "reply-1"}

	handled := false
	capture.SetHandlers(func(*feishu.InboundMessage) { handled = true }, nil, nil, nil, nil)
	base.onMessage(&feishu.InboundMessage{})
	if !handled {
		t.Fatal("SetHandlers() should delegate")
	}
	if err := capture.Start(context.Background()); err != context.Canceled {
		t.Fatalf("Start() error = %v", err)
	}
	capture.Stop()
	if !base.stopped {
		t.Fatal("Stop() should delegate")
	}
	capture.ConfigureLocalFileLinks("/tmp/state", "/repo")
	if base.localFileLinkStatePath != "/tmp/state" || base.localFileLinkProcessCWD != "/repo" {
		t.Fatalf("ConfigureLocalFileLinks() = %q %q", base.localFileLinkStatePath, base.localFileLinkProcessCWD)
	}
	if text, err := capture.RewriteLocalFileLinks(context.Background(), feishu.LocalFileLinkRewriteRequest{Text: "x"}); err != nil || text != "preview" {
		t.Fatalf("RewriteLocalFileLinks() = %q, %v", text, err)
	}
	if result, err := capture.CleanupArtifactsBefore(context.Background(), time.Now()); err != nil || result.DeletedFileCount != 1 {
		t.Fatalf("CleanupArtifactsBefore() = %+v, %v", result, err)
	}
	if err := capture.AddReaction(context.Background(), "msg-1", "SMILE"); err != nil {
		t.Fatalf("AddReaction() error = %v", err)
	}
	if err := capture.RemoveReaction(context.Background(), "msg-1", "SMILE"); err != nil {
		t.Fatalf("RemoveReaction() error = %v", err)
	}
	if err := capture.ReplyText(context.Background(), "msg-1", " hello ", false); err != nil || capture.text != "hello" || capture.card != nil {
		t.Fatalf("ReplyText() = text:%q card:%v err:%v", capture.text, capture.card, err)
	}
	if id, err := capture.ReplyTextWithID(context.Background(), "msg-1", " hi ", false); err != nil || id != "reply-1" || capture.text != "hi" {
		t.Fatalf("ReplyTextWithID() = %q %q %v", id, capture.text, err)
	}
	if err := capture.SendText(context.Background(), "chat-1", " send "); err != nil || capture.text != "send" {
		t.Fatalf("SendText() = %q %v", capture.text, err)
	}
	if id, err := capture.ReplyCard(context.Background(), "msg-1", map[string]any{"k": "v"}, false); err != nil || id != "reply-1" || capture.card == nil || capture.text != "" {
		t.Fatalf("ReplyCard() = %q card:%v text:%q err:%v", id, capture.card, capture.text, err)
	}
	if id, err := capture.SendCard(context.Background(), "chat-1", map[string]any{"k2": "v2"}); err != nil || id != "reply-1" || capture.card == nil {
		t.Fatalf("SendCard() = %q card:%v err:%v", id, capture.card, err)
	}
	if err := capture.PatchCard(context.Background(), "msg-1", map[string]any{"k3": "v3"}); err != nil || capture.card == nil {
		t.Fatalf("PatchCard() = card:%v err:%v", capture.card, err)
	}
	if path, name, err := capture.DownloadMessageResource(context.Background(), "msg-1", feishu.Attachment{Kind: "file"}, "/tmp"); err != nil || path != "/tmp/file" || name != "name.txt" {
		t.Fatalf("DownloadMessageResource() = %q %q %v", path, name, err)
	}
	if result, err := capture.ShareLocalFile(context.Background(), feishu.SharedFileRequest{LocalPath: "/tmp/file"}); err != nil || result.FileName != "name.txt" {
		t.Fatalf("ShareLocalFile() = %+v, %v", result, err)
	}
	if text, attachments, err := capture.ResolveMergeForward(context.Background(), "msg-1", []string{"a"}); err != nil || text != "merged" || len(attachments) != 0 {
		t.Fatalf("ResolveMergeForward() = %q %+v %v", text, attachments, err)
	}
	if card := capture.SimpleStatusCard("title", "blue", "body", nil); card == nil {
		t.Fatal("SimpleStatusCard() should delegate")
	}

	if chatType, chatID, rootID, userID := parseSessionKeyMeta("bad"); chatType != "" || chatID != "" || rootID != "" || userID != "" {
		t.Fatalf("parseSessionKeyMeta(bad) = %q %q %q %q", chatType, chatID, rootID, userID)
	}
	if chatType, chatID, rootID, userID := parseSessionKeyMeta("feishu:group:chat-1:root:root-1"); chatType != "group" || chatID != "chat-1" || rootID != "root-1" || userID != "" {
		t.Fatalf("parseSessionKeyMeta(group) = %q %q %q %q", chatType, chatID, rootID, userID)
	}
	if chatType, chatID, rootID, userID := parseSessionKeyMeta("feishu:p2p:chat-1:user-1"); chatType != "p2p" || chatID != "chat-1" || rootID != "" || userID != "user-1" {
		t.Fatalf("parseSessionKeyMeta(p2p) = %q %q %q %q", chatType, chatID, rootID, userID)
	}
	if chatType, chatID, rootID, userID := parseSessionKeyMeta("feishu:frontend:codex-main:group:chat-2:root:root-2"); chatType != "group" || chatID != "chat-2" || rootID != "root-2" || userID != "" {
		t.Fatalf("parseSessionKeyMeta(frontend group) = %q %q %q %q", chatType, chatID, rootID, userID)
	}
	if chatType, chatID, rootID, userID := parseSessionKeyMeta("feishu:frontend:claude-main:p2p:chat-2:user-2"); chatType != "p2p" || chatID != "chat-2" || rootID != "" || userID != "user-2" {
		t.Fatalf("parseSessionKeyMeta(frontend p2p) = %q %q %q %q", chatType, chatID, rootID, userID)
	}
}

func TestAppStateFacadeScopesPendingAndMessageLinksByFrontend(t *testing.T) {
	a, _, _ := newTestApp(t)
	facade := &appStateFacade{store: a.store, frontendID: "frontend-a"}

	if err := a.store.UpsertPending(&state.PendingRequest{FrontendID: "frontend-a", ID: "req-1", Status: "pending"}); err != nil {
		t.Fatalf("UpsertPending(frontend-a) error = %v", err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{FrontendID: "frontend-b", ID: "req-1", Status: "pending"}); err != nil {
		t.Fatalf("UpsertPending(frontend-b) error = %v", err)
	}
	if got := facade.pending("req-1"); got == nil || got.FrontendID != "frontend-a" {
		t.Fatalf("pending(req-1) = %+v, want frontend-a", got)
	}
	if got := facade.pendingRequests(); len(got) != 1 || got[0].FrontendID != "frontend-a" {
		t.Fatalf("pendingRequests() = %+v, want only frontend-a", got)
	}

	if err := a.store.UpsertMessageLink(&state.MessageLink{FrontendID: "frontend-a", MessageID: "msg-1", TurnID: "turn-a"}); err != nil {
		t.Fatalf("UpsertMessageLink(frontend-a) error = %v", err)
	}
	if err := a.store.UpsertMessageLink(&state.MessageLink{FrontendID: "frontend-b", MessageID: "msg-1", TurnID: "turn-b"}); err != nil {
		t.Fatalf("UpsertMessageLink(frontend-b) error = %v", err)
	}
	if got := facade.messageLink("msg-1"); got == nil || got.TurnID != "turn-a" {
		t.Fatalf("messageLink(msg-1) = %+v, want frontend-a link", got)
	}
}

func TestAdditionalCardAndThreadWrappers(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sessionKey := "feishu:group:chat-1:root:root-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ChatID:         "chat-1",
		ChatType:       "group",
		ActiveThreadID: "thread-1",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-1",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		ThreadID:         "thread-1",
		TurnID:           "turn-1",
		TriggerMessageID: "msg-1",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	sub := a.store.GetSubmission(subID)
	if !a.replyInThreadForSubmission(sub) {
		t.Fatal("replyInThreadForSubmission() should be true for group session")
	}
	if got := newOutboundCardService(a).sendPlanCard(context.Background(), sub, "  do thing  "); got != "reply-card-id" {
		t.Fatalf("sendPlanCard() = %q", got)
	}
	if got := newOutboundCardService(a).sendTurnEventCard(context.Background(), sub, "状态更新", "blue", "  body  ", "turn_terminal", "item-1"); got != "reply-card-id" {
		t.Fatalf("sendTurnEventCard() = %q", got)
	}
	if len(ff.replyCards) < 2 {
		t.Fatalf("replyCards = %d, want at least 2", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "计划:\ndo thing") {
		t.Fatalf("plan card body = %q", body)
	}
	if body := cardMarkdownContent(t, ff.replyCards[1]); !strings.Contains(body, "body") {
		t.Fatalf("event card body = %q", body)
	}

	card := a.cardRenderer().renderReplyMarkdownCard(sub, "", "green", "hello", nil)
	if body := cardMarkdownContent(t, card); !strings.Contains(body, "hello") {
		t.Fatalf("renderReplyMarkdownCard() body = %q", body)
	}
	card = a.cardRenderer().renderReplyMarkdownCardWithOptions(context.Background(), sub, "", "green", "", nil, false)
	if body := cardMarkdownContent(t, card); strings.TrimSpace(body) != "" {
		t.Fatalf("renderReplyMarkdownCardWithOptions(empty) body = %q", body)
	}
	if got := appendFooterText(" body ", []string{" line-1 ", "", "line-2"}); got != "body\nline-1\nline-2" {
		t.Fatalf("appendFooterText() = %q", got)
	}
	if body := cardMarkdownContent(t, a.renderUpgradeFailedCard("sess-1", " boom ")); !strings.Contains(body, "检查升级信息失败。") || !strings.Contains(body, "错误: boom") {
		t.Fatalf("renderUpgradeFailedCard() body = %q", body)
	}

	setSessionThreadDefaults(nil, "never", "read-only")
	sess := &state.Session{}
	setSessionThreadDefaults(sess, " never ", " read-only ")
	if sess.ActiveThreadApprovalPolicy != "never" || sess.ActiveThreadSandboxMode != "read-only" {
		t.Fatalf("setSessionThreadDefaults() = %+v", sess)
	}

	msg := &feishu.InboundMessage{ChatType: "group", ChatID: "chat-1", RootMessageID: "root-1", MessageID: "msg-1"}
	if _, _, _, threadID, err := a.currentThreadForMessage(msg); err != nil || threadID != "thread-1" {
		t.Fatalf("currentThreadForMessage() = %q, %v", threadID, err)
	}
	if _, _, _, _, err := (&App{cfg: a.cfg, store: a.store}).currentThreadForMessage(&feishu.InboundMessage{ChatType: "p2p", ChatID: "chat-2", UserID: "user-2"}); err == nil || !strings.Contains(err.Error(), "当前没有活动线程") {
		t.Fatalf("currentThreadForMessage(no thread) error = %v", err)
	}
}
