package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"feidex/internal/app/appcore"
	"feidex/internal/app/appstate"
	"feidex/internal/app/sessionctx"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestAppStateStoreBranches(t *testing.T) {
	var nilFacade *appstate.Store
	if got := nilFacade.Session("sess"); got != nil {
		t.Fatalf("nil Session() = %+v, want nil", got)
	}
	if got := nilFacade.Sessions(); got != nil {
		t.Fatalf("nil Sessions() = %+v, want nil", got)
	}
	if err := nilFacade.SaveSession(&state.Session{}); err != nil {
		t.Fatalf("nil SaveSession() error = %v", err)
	}
	if id, err := nilFacade.CreateSubmission(&state.Submission{}); id != "" || err != nil {
		t.Fatalf("nil CreateSubmission() = %q, %v", id, err)
	}
	nilFacade.DeleteSubmission("sub")
	if got := nilFacade.Submission("sub"); got != nil {
		t.Fatalf("nil Submission() = %+v, want nil", got)
	}
	if err := nilFacade.UpdateSubmission("sub", func(*state.Submission) {}); err != nil {
		t.Fatalf("nil UpdateSubmission() error = %v", err)
	}
	if err := nilFacade.QueueSubmission("sess", "sub"); err != nil {
		t.Fatalf("nil QueueSubmission() error = %v", err)
	}
	if got, err := nilFacade.DequeueSubmission("sess"); got != "" || err != nil {
		t.Fatalf("nil DequeueSubmission() = %q, %v", got, err)
	}
	if got := nilFacade.Pending("req"); got != nil {
		t.Fatalf("nil Pending() = %+v, want nil", got)
	}
	if err := nilFacade.SavePending(&state.PendingRequest{}); err != nil {
		t.Fatalf("nil SavePending() error = %v", err)
	}
	if got := nilFacade.PendingRequests(); got != nil {
		t.Fatalf("nil PendingRequests() = %+v, want nil", got)
	}
	if err := nilFacade.UpdatePending("req", func(*state.PendingRequest) {}); err != nil {
		t.Fatalf("nil UpdatePending() error = %v", err)
	}
	nilFacade.DeletePendingRequests(nil)
	if got, err := nilFacade.NextLocalID("x"); got != "" || err != nil {
		t.Fatalf("nil NextLocalID() = %q, %v", got, err)
	}
	if got := nilFacade.MessageLink("msg"); got != nil {
		t.Fatalf("nil MessageLink() = %+v, want nil", got)
	}
	if err := nilFacade.SaveMessageLink(&state.MessageLink{}); err != nil {
		t.Fatalf("nil SaveMessageLink() error = %v", err)
	}
	nilFacade.DeleteMessageLinks(nil)

	a, _, _ := newTestApp(t)
	facade := a.State()
	if facade == nil || facade.Store != a.store {
		t.Fatalf(".State() = %+v", facade)
	}
	if err := facade.SaveSession(&state.Session{Key: "sess-1", WorkspaceID: a.cfg.Workspaces[0].ID}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	if got := facade.Session(" sess-1 "); got == nil || got.Key != "sess-1" {
		t.Fatalf("Session() = %+v", got)
	}
	if got := facade.Sessions(); len(got) == 0 {
		t.Fatalf("Sessions() = %+v", got)
	}

	subID, err := facade.CreateSubmission(&state.Submission{SessionKey: "sess-1", WorkspaceID: a.cfg.Workspaces[0].ID})
	if err != nil || subID == "" {
		t.Fatalf("CreateSubmission() = %q, %v", subID, err)
	}
	if got := facade.Submission(subID); got == nil || got.ID != subID {
		t.Fatalf("Submission() = %+v", got)
	}
	if err := facade.SetSubmissionStatus(subID, "queued"); err != nil {
		t.Fatalf("SetSubmissionStatus() error = %v", err)
	}
	if err := facade.MarkSubmissionRunning(subID, "thread-1", "turn-1"); err != nil {
		t.Fatalf("MarkSubmissionRunning() error = %v", err)
	}
	if err := facade.FinalizeSubmission(subID, "done"); err != nil {
		t.Fatalf("FinalizeSubmission() error = %v", err)
	}
	if got := facade.Submission(subID); got == nil || got.Status != "done" || !got.Finalized || got.ThreadID != "thread-1" || got.TurnID != "turn-1" {
		t.Fatalf("Submission after FinalizeSubmission() = %+v", got)
	}
	facade.DeleteSubmission(" " + subID + " ")
	if got := facade.Submission(subID); got != nil {
		t.Fatalf("DeleteSubmission() should remove, got %+v", got)
	}

	if err := facade.QueueSubmission(" sess-1 ", " sub-2 "); err != nil {
		t.Fatalf("QueueSubmission() error = %v", err)
	}
	if got, err := facade.DequeueSubmission("sess-1"); err != nil || got != "sub-2" {
		t.Fatalf("DequeueSubmission() = %q, %v", got, err)
	}

	if err := facade.SavePending(&state.PendingRequest{ID: "req-1", Status: "pending"}); err != nil {
		t.Fatalf("SavePending() error = %v", err)
	}
	if got := facade.Pending(" req-1 "); got == nil || got.ID != "req-1" {
		t.Fatalf("Pending() = %+v", got)
	}
	if got := facade.PendingRequests(); len(got) != 1 {
		t.Fatalf("PendingRequests() = %+v", got)
	}
	if err := facade.UpdatePending("req-1", func(req *state.PendingRequest) { req.Status = "done" }); err != nil {
		t.Fatalf("UpdatePending() error = %v", err)
	}
	if got := facade.ResolvePending("req-1"); got == nil || got.Status != "resolved" {
		t.Fatalf("ResolvePending() = %+v", got)
	}
	facade.DeletePendingRequests(func(req *state.PendingRequest) bool { return req != nil && req.ID == "req-1" })
	if got := facade.Pending("req-1"); got != nil {
		t.Fatalf("DeletePendingRequests() should remove, got %+v", got)
	}

	localID, err := facade.NextLocalID(" x ")
	if err != nil || !strings.HasPrefix(localID, "x-") {
		t.Fatalf("NextLocalID() = %q, %v", localID, err)
	}

	if err := facade.SaveMessageLink(&state.MessageLink{MessageID: "msg-1", TurnID: "turn-1"}); err != nil {
		t.Fatalf("SaveMessageLink() error = %v", err)
	}
	if got := facade.MessageLink(" msg-1 "); got == nil || got.TurnID != "turn-1" {
		t.Fatalf("MessageLink() = %+v", got)
	}
	facade.DeleteMessageLinks(func(link *state.MessageLink) bool { return link != nil && link.MessageID == "msg-1" })
	if got := facade.MessageLink("msg-1"); got != nil {
		t.Fatalf("DeleteMessageLinks() should remove, got %+v", got)
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
	if err := client.ReplyLocalAttachment(context.Background(), "msg-1", "/tmp/file.txt", false); err != nil || len(base.replyLocalAttachmentCalls) != 1 {
		t.Fatalf("ReplyLocalAttachment() error = %v, calls=%v", err, base.replyLocalAttachmentCalls)
	}
	if err := client.ReplyLocalImage(context.Background(), "msg-1", "/tmp/image.png", false); err != nil || len(base.replyLocalImageCalls) != 1 {
		t.Fatalf("ReplyLocalImage() error = %v, calls=%v", err, base.replyLocalImageCalls)
	}
	if err := client.ReplyLocalVideo(context.Background(), "msg-1", "/tmp/video.mp4", false); err != nil || len(base.replyLocalVideoCalls) != 1 {
		t.Fatalf("ReplyLocalVideo() error = %v, calls=%v", err, base.replyLocalVideoCalls)
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
	if key := client.PermissionIssueKey(feishuNotifyTarget{ChatID: "chat-1", MessageID: "msg-1"}, &feishu.PermissionIssue{API: "im.message.create", Code: 1, Message: "denied", LogID: "log-1"}); !strings.Contains(key, "chat-1|msg-1|im.message.create|1|denied|log-1") {
		t.Fatalf("PermissionIssueKey() = %q", key)
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
	capture := &commandCaptureClient{Base: base, ReplyMessageID: "reply-1"}

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
	if err := capture.ReplyText(context.Background(), "msg-1", " hello ", false); err != nil || capture.Text != "hello" || capture.Card != nil {
		t.Fatalf("ReplyText() = text:%q card:%v err:%v", capture.Text, capture.Card, err)
	}
	if id, err := capture.ReplyTextWithID(context.Background(), "msg-1", " hi ", false); err != nil || id != "reply-1" || capture.Text != "hi" {
		t.Fatalf("ReplyTextWithID() = %q %q %v", id, capture.Text, err)
	}
	if err := capture.SendText(context.Background(), "chat-1", " send "); err != nil || capture.Text != "send" {
		t.Fatalf("SendText() = %q %v", capture.Text, err)
	}
	if id, err := capture.ReplyCard(context.Background(), "msg-1", map[string]any{"k": "v"}, false); err != nil || id != "reply-1" || capture.Card == nil || capture.Text != "" {
		t.Fatalf("ReplyCard() = %q card:%v text:%q err:%v", id, capture.Card, capture.Text, err)
	}
	if id, err := capture.SendCard(context.Background(), "chat-1", map[string]any{"k2": "v2"}); err != nil || id != "reply-1" || capture.Card == nil {
		t.Fatalf("SendCard() = %q card:%v err:%v", id, capture.Card, err)
	}
	if err := capture.PatchCard(context.Background(), "msg-1", map[string]any{"k3": "v3"}); err != nil || capture.Card == nil {
		t.Fatalf("PatchCard() = card:%v err:%v", capture.Card, err)
	}
	if err := capture.ReplyLocalAttachment(context.Background(), "msg-1", "/tmp/file.txt", false); err != nil || len(base.replyLocalAttachmentCalls) != 1 {
		t.Fatalf("ReplyLocalAttachment() = %v calls=%v", err, base.replyLocalAttachmentCalls)
	}
	if err := capture.ReplyLocalImage(context.Background(), "msg-1", "/tmp/image.png", false); err != nil || len(base.replyLocalImageCalls) != 1 {
		t.Fatalf("ReplyLocalImage() = %v calls=%v", err, base.replyLocalImageCalls)
	}
	if err := capture.ReplyLocalVideo(context.Background(), "msg-1", "/tmp/video.mp4", false); err != nil || len(base.replyLocalVideoCalls) != 1 {
		t.Fatalf("ReplyLocalVideo() = %v calls=%v", err, base.replyLocalVideoCalls)
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

func TestAppStateStoreScopesPendingAndMessageLinksByFrontend(t *testing.T) {
	a, _, _ := newTestApp(t)
	facade := &appstate.Store{
		AppStateFacade: appcore.AppStateFacade{
			Store:      a.store,
			FrontendID: "frontend-a",
		},
	}

	if err := a.store.UpsertPending(&state.PendingRequest{FrontendID: "frontend-a", ID: "req-1", Status: "pending"}); err != nil {
		t.Fatalf("UpsertPending(frontend-a) error = %v", err)
	}
	if err := a.store.UpsertPending(&state.PendingRequest{FrontendID: "frontend-b", ID: "req-1", Status: "pending"}); err != nil {
		t.Fatalf("UpsertPending(frontend-b) error = %v", err)
	}
	if got := facade.Pending("req-1"); got == nil || got.FrontendID != "frontend-a" {
		t.Fatalf("pending(req-1) = %+v, want frontend-a", got)
	}
	if got := facade.PendingRequests(); len(got) != 1 || got[0].FrontendID != "frontend-a" {
		t.Fatalf("pendingRequests() = %+v, want only frontend-a", got)
	}

	if err := a.store.UpsertMessageLink(&state.MessageLink{FrontendID: "frontend-a", MessageID: "msg-1", TurnID: "turn-a"}); err != nil {
		t.Fatalf("UpsertMessageLink(frontend-a) error = %v", err)
	}
	if err := a.store.UpsertMessageLink(&state.MessageLink{FrontendID: "frontend-b", MessageID: "msg-1", TurnID: "turn-b"}); err != nil {
		t.Fatalf("UpsertMessageLink(frontend-b) error = %v", err)
	}
	if got := facade.MessageLink("msg-1"); got == nil || got.TurnID != "turn-a" {
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
	if replyInThreadForSubmission(a, sub) {
		t.Fatal("replyInThreadForSubmission() should be false for group session")
	}
	if got := newOutboundCardService(a).sendPlanCardWithReuse(context.Background(), sub, "  do thing  ", ""); got != "reply-card-id" {
		t.Fatalf("sendPlanCardWithReuse() = %q", got)
	}
	if got := newOutboundCardService(a).sendTurnEventCardWithReuse(context.Background(), sub, "状态更新", "blue", "  body  ", "turn_terminal", "item-1", ""); got != "reply-card-id" {
		t.Fatalf("sendTurnEventCardWithReuse() = %q", got)
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

	card := cardRendererForApp(a).renderReplyMarkdownCard(sub, "", "green", "hello", nil)
	if body := cardMarkdownContent(t, card); !strings.Contains(body, "hello") {
		t.Fatalf("renderReplyMarkdownCard() body = %q", body)
	}
	card = cardRendererForApp(a).renderReplyMarkdownCardWithOptions(context.Background(), sub, "", "green", "", nil, false)
	if body := cardMarkdownContent(t, card); strings.TrimSpace(body) != "" {
		t.Fatalf("renderReplyMarkdownCardWithOptions(empty) body = %q", body)
	}
	if got := appendFooterText(" body ", []string{" line-1 ", "", "line-2"}); got != "body\nline-1\nline-2" {
		t.Fatalf("appendFooterText() = %q", got)
	}
	if body := cardMarkdownContent(t, newAppUpgradeService(a).renderUpgradeFailedCard("sess-1", " boom ")); !strings.Contains(body, "检查升级信息失败。") || !strings.Contains(body, "错误: boom") {
		t.Fatalf("renderUpgradeFailedCard() body = %q", body)
	}

	sessionctx.SetThreadDefaults(nil, "never", "read-only")
	sess := &state.Session{}
	sessionctx.SetThreadDefaults(sess, " never ", " read-only ")
	if sess.ActiveThreadApprovalPolicy != "never" || sess.ActiveThreadSandboxMode != "read-only" {
		t.Fatalf("setSessionThreadDefaults() = %+v", sess)
	}

	msg := &feishu.InboundMessage{ChatType: "group", ChatID: "chat-1", RootMessageID: "root-1", MessageID: "msg-1"}
	if _, _, _, threadID, err := newWorkspaceConfigService(a).currentThreadForMessage(msg); err != nil || threadID != "thread-1" {
		t.Fatalf("currentThreadForMessage() = %q, %v", threadID, err)
	}
	if _, _, _, _, err := newWorkspaceConfigService(&App{cfg: a.cfg, store: a.store}).currentThreadForMessage(&feishu.InboundMessage{ChatType: "p2p", ChatID: "chat-2", UserID: "user-2"}); err == nil || !strings.Contains(err.Error(), "当前没有活动线程") {
		t.Fatalf("currentThreadForMessage(no thread) error = %v", err)
	}
}
