package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"feidex/internal/feishu"
)

type permissionIssueTestError struct {
	err   error
	issue *feishu.PermissionIssue
}

func (e *permissionIssueTestError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *permissionIssueTestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *permissionIssueTestError) PermissionIssue() *feishu.PermissionIssue {
	if e == nil {
		return nil
	}
	return e.issue
}

func TestNotifyingFeishuClientRepliesPermissionCardForMessageTarget(t *testing.T) {
	base := &fakeFeishuClient{
		addReactionErr: &permissionIssueTestError{
			err: errors.New("permission denied"),
			issue: &feishu.PermissionIssue{
				API:            "im.message_reaction.create",
				Code:           99991668,
				Message:        "permission denied",
				LogID:          "log-1",
				Troubleshooter: "https://open.feishu.cn/troubleshoot",
				PermissionViolations: []feishu.PermissionIssueViolation{{
					Description: "缺少 scope:im:message.reaction",
				}},
				Helps: []feishu.PermissionIssueHelp{{
					URL:         "https://open.feishu.cn/app/scope",
					Description: "开通接口权限",
				}},
			},
		},
	}
	client := wrapFeishuClient(base)

	err := client.AddReaction(context.Background(), "msg-1", "SMILE")
	if err == nil {
		t.Fatal("expected AddReaction to return error")
	}
	if len(base.replyCards) != 1 {
		t.Fatalf("permission diagnostic reply cards = %d, want 1", len(base.replyCards))
	}
	body := cardMarkdownContent(t, base.replyCards[0])
	for _, want := range []string{
		"飞书接口权限或鉴权失败",
		"im.message_reaction.create",
		"99991668",
		"log-1",
		"scope:im:message.reaction",
		"[排障链接](https://open.feishu.cn/troubleshoot)",
		"[开通接口权限](https://open.feishu.cn/app/scope)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("permission diagnostic body = %q, want %q", body, want)
		}
	}
}

func TestNotifyingFeishuClientSendsPermissionCardForChatTarget(t *testing.T) {
	base := &fakeFeishuClient{
		rewriteLocalFileLinksErr: &permissionIssueTestError{
			err: errors.New("drive no permission"),
			issue: &feishu.PermissionIssue{
				API:     "drive.permission_member.create",
				Code:    99991663,
				Message: "no permission",
				Helps: []feishu.PermissionIssueHelp{{
					URL:         "https://open.feishu.cn/permissions/drive",
					Description: "开通云文档权限",
				}},
			},
		},
	}
	client := wrapFeishuClient(base)

	if _, err := client.RewriteLocalFileLinks(context.Background(), feishu.LocalFileLinkRewriteRequest{Text: "hello", ChatID: "chat-1"}); err == nil {
		t.Fatal("expected RewriteLocalFileLinks to return error")
	}
	if len(base.sendCards) != 1 {
		t.Fatalf("permission diagnostic send cards = %d, want 1", len(base.sendCards))
	}
	if body := cardMarkdownContent(t, base.sendCards[0]); !strings.Contains(body, "drive.permission_member.create") || !strings.Contains(body, "[开通云文档权限](https://open.feishu.cn/permissions/drive)") {
		t.Fatalf("permission diagnostic send body = %q", body)
	}
}

func TestNotifyingFeishuClientSendsPermissionCardForAnnouncementTarget(t *testing.T) {
	base := &fakeFeishuClient{
		announcementListErr: &feishu.AnnouncementAPIError{
			Op:   "docx.chat_announcement_block.list",
			Code: 99991672,
			Msg:  "Access denied. One of the following scopes is required: [im:chat.announcement:read].应用尚未开通所需的应用身份权限：[im:chat.announcement:read]，点击链接申请并开通任一权限即可：https://open.feishu.cn/app/cli_a945cd72cafb1cb5/auth?q=im:chat.announcement:read&op_from=openapi&token_type=tenant",
		},
	}
	client := wrapFeishuClient(base)

	if _, err := client.ListAnnouncementBlocks(context.Background(), "chat-1"); err == nil {
		t.Fatal("expected ListAnnouncementBlocks to return error")
	}
	if len(base.sendCards) != 1 {
		t.Fatalf("permission diagnostic send cards = %d, want 1", len(base.sendCards))
	}
	body := cardMarkdownContent(t, base.sendCards[0])
	for _, want := range []string{
		"飞书接口权限或鉴权失败",
		"docx.chat_announcement_block.list",
		"99991672",
		"im:chat.announcement:read",
		"[申请权限](https://open.feishu.cn/app/cli_a945cd72cafb1cb5/auth?q=im:chat.announcement:read&op_from=openapi&token_type=tenant)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("permission diagnostic body = %q, want %q", body, want)
		}
	}
}

func TestNotifyingFeishuClientUrgentAppUsesExplicitUserID(t *testing.T) {
	base := &fakeFeishuClient{
		sendCardID: "sent-card-1",
		rewriteLocalFileLinksErr: &permissionIssueTestError{
			err: errors.New("drive no permission"),
			issue: &feishu.PermissionIssue{
				API:     "drive.permission_member.create",
				Code:    99991663,
				Message: "no permission",
			},
		},
	}
	client := wrapFeishuClient(base)

	if _, err := client.RewriteLocalFileLinks(context.Background(), feishu.LocalFileLinkRewriteRequest{
		Text:   "hello",
		ChatID: "chat-1",
		UserID: "ou-user-1",
	}); err == nil {
		t.Fatal("expected RewriteLocalFileLinks to return error")
	}
	if len(base.urgentAppCalls) != 1 {
		t.Fatalf("urgent app calls = %+v, want 1", base.urgentAppCalls)
	}
	if got := base.urgentAppCalls[0]; got.messageID != "sent-card-1" || got.userID != "ou-user-1" {
		t.Fatalf("urgent app call = %+v, want sent-card-1/ou-user-1", got)
	}
	if len(base.lookupMessageSenderCalls) != 0 {
		t.Fatalf("lookup sender calls = %+v, want none for explicit user id", base.lookupMessageSenderCalls)
	}
}

func TestNotifyingFeishuClientUrgentAppLooksUpMessageSender(t *testing.T) {
	base := &fakeFeishuClient{
		replyCardID:             "reply-card-1",
		lookupMessageSenderOpen: "ou-msg-owner",
		addReactionErr: &permissionIssueTestError{
			err: errors.New("permission denied"),
			issue: &feishu.PermissionIssue{
				API:     "im.message_reaction.create",
				Code:    99991668,
				Message: "permission denied",
			},
		},
	}
	client := wrapFeishuClient(base)

	if err := client.AddReaction(context.Background(), "msg-1", "SMILE"); err == nil {
		t.Fatal("expected AddReaction to return error")
	}
	if len(base.lookupMessageSenderCalls) != 1 || base.lookupMessageSenderCalls[0] != "msg-1" {
		t.Fatalf("lookup sender calls = %+v, want [msg-1]", base.lookupMessageSenderCalls)
	}
	if len(base.urgentAppCalls) != 1 {
		t.Fatalf("urgent app calls = %+v, want 1", base.urgentAppCalls)
	}
	if got := base.urgentAppCalls[0]; got.messageID != "reply-card-1" || got.userID != "ou-msg-owner" {
		t.Fatalf("urgent app call = %+v, want reply-card-1/ou-msg-owner", got)
	}
}

func TestNotifyingFeishuClientSkipsUrgentAppWithoutUserID(t *testing.T) {
	base := &fakeFeishuClient{
		sendCardID: "sent-card-1",
		rewriteLocalFileLinksErr: &permissionIssueTestError{
			err: errors.New("drive no permission"),
			issue: &feishu.PermissionIssue{
				API:     "drive.permission_member.create",
				Code:    99991663,
				Message: "no permission",
			},
		},
	}
	client := wrapFeishuClient(base)

	if _, err := client.RewriteLocalFileLinks(context.Background(), feishu.LocalFileLinkRewriteRequest{
		Text:   "hello",
		ChatID: "chat-1",
	}); err == nil {
		t.Fatal("expected RewriteLocalFileLinks to return error")
	}
	if len(base.urgentAppCalls) != 0 {
		t.Fatalf("urgent app calls = %+v, want none", base.urgentAppCalls)
	}
	if len(base.lookupMessageSenderCalls) != 0 {
		t.Fatalf("lookup sender calls = %+v, want none", base.lookupMessageSenderCalls)
	}
}

func TestNotifyingFeishuClientDeduplicatesRecentPermissionCards(t *testing.T) {
	base := &fakeFeishuClient{
		removeReactionErr: &permissionIssueTestError{
			err: errors.New("permission denied"),
			issue: &feishu.PermissionIssue{
				API:     "im.message_reaction.delete",
				Code:    99991668,
				Message: "permission denied",
			},
		},
	}
	client := wrapFeishuClient(base)

	if err := client.RemoveReaction(context.Background(), "msg-1", "SMILE"); err == nil {
		t.Fatal("expected first RemoveReaction to fail")
	}
	if err := client.RemoveReaction(context.Background(), "msg-1", "SMILE"); err == nil {
		t.Fatal("expected second RemoveReaction to fail")
	}
	if len(base.replyCards) != 1 {
		t.Fatalf("deduplicated permission diagnostic cards = %d, want 1", len(base.replyCards))
	}
}
