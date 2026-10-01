package feishu

import "testing"

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
