package feishu

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"feidex/internal/config"
)

func strPtr(v string) *string {
	return &v
}

func TestNewConfiguresAllowListAndLifecycleHelpers(t *testing.T) {
	a := New(config.FeishuConfig{AllowFrom: []string{" user-1 ", "", "*", "user-2"}})
	if !a.allowAll {
		t.Fatal("expected wildcard allow list to enable allowAll")
	}
	if _, ok := a.allowSet["user-1"]; !ok {
		t.Fatalf("allowSet missing explicit user: %+v", a.allowSet)
	}

	called := false
	a.SetHandlers(func(*InboundMessage) {}, nil, nil, nil, nil)
	a.cancel = func() { called = true }
	a.Stop()
	if !called {
		t.Fatal("Stop() did not invoke cancel")
	}

	a.ConfigureMarkdownPreview(" state.json ", " /repo ")
	if a.previewStatePath != "state.json" || a.previewProcessCWD != "/repo" || a.previewer != nil {
		t.Fatalf("ConfigureMarkdownPreview() did not trim/reset fields: %+v", a)
	}
}

func TestAuthFailureHelpersAndLogging(t *testing.T) {
	if !isFeishuAuthOrPermissionFailure(errors.New("token expired"), "") {
		t.Fatal("expected auth keyword in error to count as auth failure")
	}
	if !isFeishuAuthOrPermissionFailure(nil, "no permission to access resource") {
		t.Fatal("expected permission keyword in API message to count as auth failure")
	}
	if isFeishuAuthOrPermissionFailure(nil, "temporary unavailable") {
		t.Fatal("unexpected auth failure classification")
	}

	logWithLevel(-4, "debug log")
	logWithLevel(0, "info log")
	logWithLevel(4, "warn log")
	logWithLevel(8, "error log")
	logFeishuFailure("failure", errors.New("token invalid"), 0, "", "op", "test")
	logFeishuFailure("failure", nil, 403, "forbidden", "op", "test")
}

func TestSimpleStatusCardAndSummaries(t *testing.T) {
	a := &Adapter{}
	card := a.SimpleStatusCard("Title", "green", "Body text", []Button{
		{Text: "Open", Type: "primary", Name: "open", Value: map[string]any{"id": "1"}},
		{Text: "Cancel", Type: "default", Value: map[string]any{"id": "2"}},
	})
	title, preview, buttonCount := summarizeCardForLog(card)
	if title != "Title" || preview != "Body text" || buttonCount != 2 {
		t.Fatalf("summarizeCardForLog() = %q, %q, %d", title, preview, buttonCount)
	}
	elements := card["elements"].([]map[string]any)
	if len(elements) != 2 {
		t.Fatalf("SimpleStatusCard() elements = %+v, want markdown + action", elements)
	}

	bodyCard := map[string]any{
		"header": map[string]any{
			"title": map[string]any{"content": "Nested"},
		},
		"body": map[string]any{
			"elements": []map[string]any{
				{"tag": "markdown", "content": "Nested body"},
				{"tag": "action", "actions": []map[string]any{{"tag": "button"}}},
			},
		},
	}
	title, preview, buttonCount = summarizeCardForLog(bodyCard)
	if title != "Nested" || preview != "Nested body" || buttonCount != 1 {
		t.Fatalf("nested summarizeCardForLog() = %q, %q, %d", title, preview, buttonCount)
	}
}

func TestConvertMessageTextFlow(t *testing.T) {
	a := New(config.FeishuConfig{GroupAtOnly: true})
	a.botOpenID = "bot-1"

	msgType := "text"
	chatType := "group"
	content := `{"text":"@bot hello"}`
	messageID := "msg-1"
	rootID := "root-1"
	parentID := "parent-1"
	chatID := "chat-1"
	threadID := "thread-1"
	createTime := "1700000123456"
	mentionKey := "@bot"
	userID := "user-1"

	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: &userID},
			},
			Message: &larkim.EventMessage{
				MessageId:   &messageID,
				RootId:      &rootID,
				ParentId:    &parentID,
				ChatId:      &chatID,
				ThreadId:    &threadID,
				ChatType:    &chatType,
				MessageType: &msgType,
				Content:     &content,
				CreateTime:  &createTime,
				Mentions: []*larkim.MentionEvent{
					{Key: &mentionKey, Id: &larkim.UserId{OpenId: strPtr("bot-1")}},
				},
			},
		},
	}

	got := a.convertMessage(event)
	if got == nil {
		t.Fatal("convertMessage(text) returned nil")
	}
	if got.Text != "hello" || got.MessageID != "msg-1" || got.RootMessageID != "root-1" || got.ParentMessageID != "parent-1" {
		t.Fatalf("convertMessage(text) = %+v, want stripped text and root/parent ids", got)
	}
	if got.ThreadID != "thread-1" || got.CreatedAt != 1700000123 {
		t.Fatalf("convertMessage(text) missing thread or createdAt: %+v", got)
	}

	if duplicate := a.convertMessage(event); duplicate != nil {
		t.Fatalf("expected duplicate message to be suppressed, got %+v", duplicate)
	}

	noMention := New(config.FeishuConfig{GroupAtOnly: true})
	noMention.botOpenID = "bot-1"
	if got := noMention.convertMessage(&larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: &userID}},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("msg-2"),
				ChatType:    &chatType,
				MessageType: &msgType,
				Content:     &content,
			},
		},
	}); got != nil {
		t.Fatalf("expected group message without bot mention to be ignored, got %+v", got)
	}

	everyoneKey := "@all"
	everyoneName := "所有人"
	allAdapter := New(config.FeishuConfig{GroupAtOnly: true, RespondToAtEveryone: true})
	allAdapter.botOpenID = "bot-1"
	got = allAdapter.convertMessage(&larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: &userID}},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("msg-3"),
				ChatType:    &chatType,
				MessageType: &msgType,
				Content:     strPtr(`{"text":"@all ping"}`),
				Mentions:    []*larkim.MentionEvent{{Key: &everyoneKey, Name: &everyoneName}},
			},
		},
	})
	if got == nil || got.Text != "@all ping" {
		t.Fatalf("expected @all message to pass through, got %+v", got)
	}
}

func TestConvertMessageAttachmentsRecallAndReaction(t *testing.T) {
	a := New(config.FeishuConfig{})
	userID := "user-1"
	chatType := "p2p"

	imageType := "image"
	imageContent := `{"image_key":"img-key"}`
	imageMsg := a.convertMessage(&larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: &userID}},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("img-msg"),
				ChatType:    &chatType,
				MessageType: &imageType,
				Content:     &imageContent,
			},
		},
	})
	if imageMsg == nil || len(imageMsg.Attachments) != 1 || imageMsg.Attachments[0].Kind != "image" {
		t.Fatalf("convertMessage(image) = %+v, want image attachment", imageMsg)
	}

	fileType := "file"
	fileContent := `{"file_key":"file-key"}`
	fileMsg := a.convertMessage(&larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: &userID}},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("file-msg"),
				ChatType:    &chatType,
				MessageType: &fileType,
				Content:     &fileContent,
			},
		},
	})
	if fileMsg == nil || fileMsg.Attachments[0].Kind != "file" {
		t.Fatalf("convertMessage(file) = %+v, want file attachment", fileMsg)
	}

	audioType := "audio"
	audioContent := `{"file_key":"audio-key"}`
	audioMsg := a.convertMessage(&larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: &userID}},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("audio-msg"),
				ChatType:    &chatType,
				MessageType: &audioType,
				Content:     &audioContent,
			},
		},
	})
	if audioMsg == nil || audioMsg.Attachments[0].Kind != "audio" {
		t.Fatalf("convertMessage(audio) = %+v, want audio attachment", audioMsg)
	}

	recall := a.convertMessageRecall(&larkim.P2MessageRecalledV1{
		Event: &larkim.P2MessageRecalledV1Data{
			MessageId: strPtr("msg-1"),
			ChatId:    strPtr("chat-1"),
		},
	})
	if recall == nil || recall.MessageID != "msg-1" || recall.ChatID != "chat-1" {
		t.Fatalf("convertMessageRecall() = %+v, want ids", recall)
	}

	reaction := a.convertMessageReaction(&larkim.P2MessageReactionCreatedV1{
		Event: &larkim.P2MessageReactionCreatedV1Data{
			MessageId:    strPtr("msg-1"),
			OperatorType: strPtr("user"),
			ReactionType: &larkim.Emoji{EmojiType: strPtr("SMILE")},
			UserId:       &larkim.UserId{OpenId: strPtr("open-1")},
		},
	})
	if reaction == nil || reaction.MessageID != "msg-1" || reaction.UserID != "open-1" || reaction.EmojiType != "SMILE" {
		t.Fatalf("convertMessageReaction() = %+v, want parsed reaction", reaction)
	}

	if got := a.convertMessageReaction(&larkim.P2MessageReactionCreatedV1{
		Event: &larkim.P2MessageReactionCreatedV1Data{
			MessageId:    strPtr("msg-1"),
			OperatorType: strPtr("app"),
			ReactionType: &larkim.Emoji{EmojiType: strPtr("SMILE")},
		},
	}); got != nil {
		t.Fatalf("expected non-user reaction to be ignored, got %+v", got)
	}
}

func TestAdapterHelperFunctions(t *testing.T) {
	a := &Adapter{
		allowSet: map[string]struct{}{"user-1": {}},
		seen: map[string]time.Time{
			"expired": time.Now().Add(-11 * time.Minute),
		},
	}
	if !a.allowed("user-1") || a.allowed("user-2") {
		t.Fatal("allowed() returned unexpected result")
	}
	if a.duplicate("msg-1") {
		t.Fatal("first duplicate() call should be false")
	}
	if !a.duplicate("msg-1") {
		t.Fatal("second duplicate() call should be true")
	}
	if _, ok := a.seen["expired"]; ok {
		t.Fatal("duplicate() should prune expired entries")
	}

	if got := extractText(strPtr(`{"text":"hello"}`)); got != "hello" {
		t.Fatalf("extractText() = %q, want hello", got)
	}
	if got := extractText(strPtr("{")); got != "" {
		t.Fatalf("extractText(invalid) = %q, want empty", got)
	}
	if got := parseUnixMillis("1700000123456"); got != 1700000123 {
		t.Fatalf("parseUnixMillis() = %d, want seconds", got)
	}
	if got := parseUnixMillis("bad"); got != 0 {
		t.Fatalf("parseUnixMillis(invalid) = %d, want 0", got)
	}

	if attachment, ok := extractImageAttachment(strPtr(`{"image_key":"img"}`)); !ok || attachment.ResourceKey != "img" {
		t.Fatalf("extractImageAttachment() = %+v, %v", attachment, ok)
	}
	if attachment, ok := extractFileAttachment(strPtr(`{"file_key":"file"}`)); !ok || attachment.ResourceKey != "file" {
		t.Fatalf("extractFileAttachment() = %+v, %v", attachment, ok)
	}
	if attachment, ok := extractAudioAttachment(strPtr(`{"file_key":"audio"}`)); !ok || attachment.ResourceKey != "audio" {
		t.Fatalf("extractAudioAttachment() = %+v, %v", attachment, ok)
	}

	mentionKey := "@bot"
	everyoneKey := "@all"
	everyoneName := "Everyone"
	mentions := []*larkim.MentionEvent{
		{Key: &mentionKey, Id: &larkim.UserId{OpenId: strPtr("bot-1")}},
		{Key: &everyoneKey, Name: &everyoneName},
	}
	if got := stripBotMention("@bot hello", mentions, "bot-1"); got != "hello" {
		t.Fatalf("stripBotMention() = %q, want hello", got)
	}
	if !mentioned(mentions, "bot-1") {
		t.Fatal("mentioned() should find bot open id")
	}
	if !mentionedEveryone(mentions) {
		t.Fatal("mentionedEveryone() should recognize @all")
	}

	if got := parseReactionUserID(&larkim.UserId{OpenId: strPtr("open"), UserId: strPtr("user"), UnionId: strPtr("union")}); got != "open" {
		t.Fatalf("parseReactionUserID(open) = %q, want open", got)
	}
	if got := parseReactionUserID(&larkim.UserId{UserId: strPtr("user"), UnionId: strPtr("union")}); got != "user" {
		t.Fatalf("parseReactionUserID(user) = %q, want user", got)
	}
	if got := parseReactionUserID(&larkim.UserId{UnionId: strPtr("union")}); got != "union" {
		t.Fatalf("parseReactionUserID(union) = %q, want union", got)
	}
	if got := parseReactionUserID(nil); got != "" {
		t.Fatalf("parseReactionUserID(nil) = %q, want empty", got)
	}
}

func TestDownloadNamingAndAttachmentHelpers(t *testing.T) {
	resp := &larkim.GetMessageResourceResp{FileName: " report.txt "}
	if got := resolveDownloadedFileName(resp, Attachment{Kind: "file", ResourceKey: "key"}); got != "report.txt" {
		t.Fatalf("resolveDownloadedFileName(fileName) = %q, want report.txt", got)
	}

	resp = &larkim.GetMessageResourceResp{
		ApiResp: &larkcore.ApiResp{
			Header: http.Header{"Content-Type": []string{"image/jpeg"}},
		},
	}
	if got := resolveDownloadedFileName(resp, Attachment{Kind: "image", ResourceKey: "key/with spaces"}); !strings.HasSuffix(got, ".jfif") && !strings.HasSuffix(got, ".jpg") && !strings.HasSuffix(got, ".jpeg") {
		t.Fatalf("resolveDownloadedFileName(content-type) = %q, want jpeg-family suffix", got)
	}

	if got := truncateForLog("  hello world  ", 5); got != "hello..." {
		t.Fatalf("truncateForLog() = %q, want hello...", got)
	}
	if got := sanitizeDownloadedFileName(" ../report.txt\x00 "); got != "report.txt" {
		t.Fatalf("sanitizeDownloadedFileName() = %q, want report.txt", got)
	}

	dir := t.TempDir()
	first := uniqueDownloadPath(dir, "report.txt")
	if first != filepath.Join(dir, "report.txt") {
		t.Fatalf("uniqueDownloadPath(first) = %q", first)
	}
	if err := os.WriteFile(first, []byte("exists"), 0o644); err != nil {
		t.Fatalf("WriteFile(existing) error = %v", err)
	}
	second := uniqueDownloadPath(dir, "report.txt")
	if second != filepath.Join(dir, "report-2.txt") {
		t.Fatalf("uniqueDownloadPath(second) = %q, want report-2.txt", second)
	}

	imagePath := filepath.Join(dir, "image.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile(image) error = %v", err)
	}
	info, err := os.Stat(imagePath)
	if err != nil {
		t.Fatalf("Stat(image) error = %v", err)
	}
	if !isSupportedReplyImage(imagePath, info) {
		t.Fatal("isSupportedReplyImage() should accept small regular images")
	}
	if isSupportedReplyImage(filepath.Join(dir, "image.txt"), info) {
		t.Fatal("isSupportedReplyImage() should reject unsupported extensions")
	}

	if got := defaultAttachmentExt("image"); got != ".png" {
		t.Fatalf("defaultAttachmentExt(image) = %q, want .png", got)
	}
	if got := defaultAttachmentExt("audio"); got != ".opus" {
		t.Fatalf("defaultAttachmentExt(audio) = %q, want .opus", got)
	}
	if got := defaultAttachmentExt("file"); got != ".bin" {
		t.Fatalf("defaultAttachmentExt(file) = %q, want .bin", got)
	}
	if got := resourceTypeForAttachment("image"); got != "image" {
		t.Fatalf("resourceTypeForAttachment(image) = %q, want image", got)
	}
	if got := resourceTypeForAttachment("audio"); got != "file" {
		t.Fatalf("resourceTypeForAttachment(audio) = %q, want file", got)
	}

	if got := detectUploadFileType("doc.pdf"); got != "pdf" {
		t.Fatalf("detectUploadFileType(pdf) = %q, want pdf", got)
	}
	if got := detectUploadFileType("report.docx"); got != "doc" {
		t.Fatalf("detectUploadFileType(docx) = %q, want doc", got)
	}
	if got := detectUploadFileType("movie.mp4"); got != "mp4" {
		t.Fatalf("detectUploadFileType(mp4) = %q, want mp4", got)
	}
	if got := detectUploadFileType("archive.zip"); got != "stream" {
		t.Fatalf("detectUploadFileType(default) = %q, want stream", got)
	}

	if got := sanitizeAttachmentKey(" key /with*bad "); got != "key__with_bad" {
		t.Fatalf("sanitizeAttachmentKey() = %q, want sanitized key", got)
	}
	if got := sanitizeAttachmentKey("   "); got != "attachment" {
		t.Fatalf("sanitizeAttachmentKey(empty) = %q, want attachment", got)
	}
	if got := reactionKey(" msg ", " smile "); got != "msg:smile" {
		t.Fatalf("reactionKey() = %q, want trimmed key", got)
	}
}

func TestFetchBotOpenIDHandlesFailuresQuickly(t *testing.T) {
	transport := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, io.EOF
	})
	defer func() { http.DefaultTransport = transport }()

	if got := (&Adapter{cfg: config.FeishuConfig{AppID: "app", AppSecret: "secret"}}).fetchBotOpenID(); got != "" {
		t.Fatalf("fetchBotOpenID() = %q, want empty on transport error", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
