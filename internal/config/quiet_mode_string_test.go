package config

import "testing"

func TestQuietModeStringNormalizesAndFallsBackToProgress(t *testing.T) {
	if got := QuietMode("").String(); got != string(QuietModeProgress) {
		t.Fatalf("QuietMode(\"\").String() = %q, want %q", got, QuietModeProgress)
	}
	if got := QuietMode(" progress ").String(); got != string(QuietModeProgress) {
		t.Fatalf("QuietMode(progress).String() = %q, want %q", got, QuietModeProgress)
	}
	if got := QuietMode("bad").String(); got != string(QuietModeProgress) {
		t.Fatalf("QuietMode(bad).String() = %q, want progress fallback", got)
	}
}
