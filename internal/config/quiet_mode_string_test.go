package config

import "testing"

func TestQuietModeStringNormalizesAndFallsBackToVerbose(t *testing.T) {
	if got := QuietMode(" progress ").String(); got != string(QuietModeProgress) {
		t.Fatalf("QuietMode(progress).String() = %q, want %q", got, QuietModeProgress)
	}
	if got := QuietMode("bad").String(); got != string(QuietModeVerbose) {
		t.Fatalf("QuietMode(bad).String() = %q, want verbose fallback", got)
	}
}
