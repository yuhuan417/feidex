package release

import (
	"context"
	"strings"
	"testing"
)

func TestNewGitHubClientAppliesDefaults(t *testing.T) {
	client := NewGitHubClient("", "", nil)
	if client.owner != DefaultRepoOwner || client.repo != DefaultRepoName || client.http == nil {
		t.Fatalf("NewGitHubClient() = %+v, want defaults", client)
	}
}

func TestLatestLinuxBinaryReportsMissingAssets(t *testing.T) {
	origGOOS := currentGOOS
	defer func() { currentGOOS = origGOOS }()
	currentGOOS = func() string { return "linux" }

	client := NewGitHubClient("test", "feidex", nil)
	client.http.Transport = stubTransport{
		responses: map[string]string{
			"https://api.github.com/repos/test/feidex/releases/latest": `{
				"tag_name":"v0.2.0",
				"assets":[
					{"name":"sha256sums.txt","browser_download_url":"https://download.test/sums"}
				]
			}`,
		},
	}
	if _, err := client.LatestLinuxBinary(context.Background(), "amd64"); err == nil || !strings.Contains(err.Error(), "missing asset feidex-linux-amd64") {
		t.Fatalf("LatestLinuxBinary() error = %v, want missing asset", err)
	}
}

func TestFetchLatestReleaseStatusError(t *testing.T) {
	client := NewGitHubClient("test", "feidex", nil)
	client.http.Transport = stubTransport{}
	if _, err := client.fetchLatestRelease(context.Background()); err == nil || !strings.Contains(err.Error(), "status=404") {
		t.Fatalf("fetchLatestRelease() error = %v, want 404", err)
	}
}

func TestFetchChecksumsAndVerifySHA256(t *testing.T) {
	client := NewGitHubClient("test", "feidex", nil)
	client.http.Transport = stubTransport{
		responses: map[string]string{
			"https://download.test/sums": "abc dist/feidex-linux-amd64\nxyz *feidex-linux-aarch64\n",
		},
	}
	values, err := client.fetchChecksums(context.Background(), "https://download.test/sums")
	if err != nil {
		t.Fatalf("fetchChecksums() error = %v", err)
	}
	if values["dist/feidex-linux-amd64"] != "abc" || values["feidex-linux-aarch64"] != "xyz" {
		t.Fatalf("fetchChecksums() = %+v, want parsed names and basenames", values)
	}

	if err := VerifySHA256([]byte("hello"), "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"); err != nil {
		t.Fatalf("VerifySHA256(valid) error = %v", err)
	}
	if err := VerifySHA256([]byte("hello"), "bad"); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("VerifySHA256(invalid) error = %v, want mismatch", err)
	}
	if err := VerifySHA256(nil, "bad"); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("VerifySHA256(empty) error = %v, want empty content", err)
	}
}

func TestParseVersionAndCompareSuffix(t *testing.T) {
	got, err := ParseVersion("v1.2.3-rc1")
	if err != nil {
		t.Fatalf("ParseVersion() error = %v", err)
	}
	if got.Major != 1 || got.Minor != 2 || got.Patch != 3 || got.Suffix != "-rc1" {
		t.Fatalf("ParseVersion() = %+v, want parsed suffix", got)
	}
	if _, err := ParseVersion("bad"); err == nil {
		t.Fatal("expected invalid version to fail")
	}

	if got := compareSuffix("", "-rc1"); got != 1 {
		t.Fatalf("compareSuffix(release, prerelease) = %d, want 1", got)
	}
	if got := compareSuffix("-alpha", "-beta"); got >= 0 {
		t.Fatalf("compareSuffix(alpha, beta) = %d, want < 0", got)
	}
	if got := compareSuffix("-rc.10", "-rc.2"); got <= 0 {
		t.Fatalf("compareSuffix(rc.10, rc.2) = %d, want > 0", got)
	}
}
