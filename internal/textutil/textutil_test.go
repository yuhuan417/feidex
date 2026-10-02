package textutil

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	if got := FirstNonEmpty("  ", " first ", "second"); got != "first" {
		t.Fatalf("FirstNonEmpty() = %q, want %q", got, "first")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name  string
		value string
		limit int
		want  string
	}{
		{name: "unchanged", value: "hello", limit: 5, want: "hello"},
		{name: "runes", value: "你好世界", limit: 3, want: "你好…"},
		{name: "one", value: "hello", limit: 1, want: "…"},
		{name: "non-positive", value: "hello", limit: 0, want: "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Truncate(tt.value, tt.limit); got != tt.want {
				t.Fatalf("Truncate(%q, %d) = %q, want %q", tt.value, tt.limit, got, tt.want)
			}
		})
	}
}
