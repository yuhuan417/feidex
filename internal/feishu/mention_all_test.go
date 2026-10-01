package feishu

import (
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func strptr(s string) *string { return &s }

// The @所有人 placeholder is a client artifact and must be stripped: the router
// only treats text starting with "/" as a command, so "@_all /menu" would
// otherwise fall through to the agent as an ordinary message.
func TestStripMentionAllPlaceholder(t *testing.T) {
	cases := []struct{ in, want string }{
		{"@_all /menu", "/menu"},
		{"@_all hihihi", "hihihi"},
		{"@_all", ""},
		{"@_all  @_all /stop", "/stop"},
		{"/menu", "/menu"},
		{"hihihi", "hihihi"},
	}
	for _, c := range cases {
		if got := stripMentionAllPlaceholder(c.in); got != c.want {
			t.Fatalf("strip(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// @所有人 does not appear in mentions[] at all — measured against a live tenant,
// that array arrives empty and the placeholder is only in the text. Detection
// therefore has to read the text.
func TestMentionsAllDetectsTextPlaceholder(t *testing.T) {
	if !mentionsAll(nil, "@_all /menu") {
		t.Fatal("empty mentions with the @_all placeholder in the text must count as @所有人")
	}
	if mentionsAll(nil, "/menu") {
		t.Fatal("a plain command must not count as @所有人")
	}
}

// A message addressing two bots must reach each of them with the same clean
// text. Stripping only this bot's own placeholder left the other bot's behind,
// so "@bot1 @bot2 /menu" arrived as "@_user_2 /menu" — not starting with "/",
// hence no command — and "@bot1 @bot2 hi" reached the agent as "@_user_2 hi".
func TestStripMentionPlaceholdersRemovesEveryPlaceholder(t *testing.T) {
	mentions := []*larkim.MentionEvent{
		{Key: strptr("@_user_1"), Id: &larkim.UserId{OpenId: strptr("ou_bot1")}},
		{Key: strptr("@_user_2"), Id: &larkim.UserId{OpenId: strptr("ou_bot2")}},
	}
	for _, c := range []struct{ in, want string }{
		{"@_user_1 @_user_2", ""},
		{"@_user_1 @_user_2 /menu", "/menu"},
		{"@_user_1 @_user_2 hi", "hi"},
		{"@_user_1 /menu", "/menu"}, // 单 mention 不变
		{"hi", "hi"},
	} {
		if got := stripMentionPlaceholders(c.in, mentions); got != c.want {
			t.Fatalf("strip(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
