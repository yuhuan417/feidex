package buildinfo

import "testing"

func TestCurrentVersionTrimsConfiguredVersion(t *testing.T) {
	orig := Version
	defer func() { Version = orig }()

	Version = "  v1.2.3  "
	if got := CurrentVersion(); got != "v1.2.3" {
		t.Fatalf("CurrentVersion() = %q, want v1.2.3", got)
	}
}

func TestCurrentVersionFallsBackToDev(t *testing.T) {
	orig := Version
	defer func() { Version = orig }()

	Version = "   "
	if got := CurrentVersion(); got != "dev" {
		t.Fatalf("CurrentVersion() = %q, want dev", got)
	}
}
