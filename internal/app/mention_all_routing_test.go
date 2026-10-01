package app

import (
	"testing"

	"feidex/internal/feishu"
)

// @所有人 addresses the whole group, so every bot answers it — including bots
// that are not primary. It must not fall into the primary-assignment branch:
// a message that is only the mention looks like a bare bot mention, but it
// names nobody.
func TestMentionAllIsDeliveredToEveryBot(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{botOpenID: "bot-open"}
	a := newGroupAnnouncementTestApp(t, store, ff, "bot-a")
	configureGroupMessagePolicy(a)
	// A group with no primary state goes through the probe path, which delivers
	// plain messages so a bot can observe the group before /primary is set.
	// Establish the state so the baseline below is the post-setup behaviour.
	if _, err := setGroupPrimaryState(a, "group", "chat-1", false, nil); err != nil {
		t.Fatalf("setGroupPrimaryState() error = %v", err)
	}

	// Primary state exists and this bot is not primary: a plain message is dropped.
	if shouldDeliverGroupMessageToApp(a, feishu.GroupMessagePolicyInput{
		ChatID: "chat-1", Text: "随便说点什么",
	}) {
		t.Fatal("plain message must not be delivered to a non-primary bot")
	}

	// @所有人 with body text.
	if !shouldDeliverGroupMessageToApp(a, feishu.GroupMessagePolicyInput{
		ChatID: "chat-1", Text: "@_all 大家看一下", MentionAll: true, MentionedAny: true,
	}) {
		t.Fatal("@所有人 must be delivered")
	}

	// @所有人 on its own — the shape that used to be swallowed as a primary
	// assignment.
	if !shouldDeliverGroupMessageToApp(a, feishu.GroupMessagePolicyInput{
		ChatID: "chat-1", Text: "@_all", MentionAll: true, MentionedAny: true,
	}) {
		t.Fatal("bare @所有人 must be delivered, not treated as a primary assignment")
	}
}

// A mention of someone specific must still not fall through to the primary bot.
func TestSpecificMentionStillBlocksDelivery(t *testing.T) {
	store := newGroupAnnouncementStore(t)
	ff := &fakeFeishuClient{botOpenID: "bot-open"}
	a := newGroupAnnouncementTestApp(t, store, ff, "bot-a")
	configureGroupMessagePolicy(a)
	if _, err := setGroupPrimaryState(a, "group", "chat-1", false, nil); err != nil {
		t.Fatalf("setGroupPrimaryState() error = %v", err)
	}

	if shouldDeliverGroupMessageToApp(a, feishu.GroupMessagePolicyInput{
		ChatID: "chat-1", Text: "@某人 你好", MentionedAny: true, MentionedOpenIDs: []string{"ou_someone"},
	}) {
		t.Fatal("mentioning someone else must not be delivered to the primary bot")
	}
}
