package carddemo

import (
	"strings"
	"testing"
)

func TestHelpers(t *testing.T) {
	cases := map[string]string{
		"agent":            "turn_output",
		"agent_message":    "turn_output",
		"final":            "final_message",
		"final_agent":      "final_message",
		"turn_terminal":    "turn_terminal",
		"turn_file_change": "turn_file_change",
	}
	for input, want := range cases {
		if got := NormalizeKind(input); got != want {
			t.Fatalf("NormalizeKind(%q) = %q, want %q", input, got, want)
		}
	}
	if got := NormalizeKind("plain"); got != "" {
		t.Fatalf("NormalizeKind(plain) = %q, want empty", got)
	}

	if got := DefaultBody("turn_command_execution"); !strings.Contains(got, "pwd") {
		t.Fatalf("DefaultBody(command) = %q", got)
	}
	if got := DefaultBody("turn_file_change"); !strings.Contains(got, "main.go") {
		t.Fatalf("DefaultBody(file_change) = %q", got)
	}
	if got := DefaultBody("unknown"); !strings.Contains(got, "卡片 demo") {
		t.Fatalf("DefaultBody(default) = %q", got)
	}
}
