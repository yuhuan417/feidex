package codex

import (
	"encoding/json"
	"testing"
)

func TestRequestIDKey(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{` "req-1" `, "req-1"},
		{`42`, "42"},
		{``, ""},
	} {
		if got := RequestIDKey(json.RawMessage(tc.raw)); got != tc.want {
			t.Errorf("RequestIDKey(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
