package release

import (
	"context"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLatestLinuxBinaryAMD64(t *testing.T) {
	origGOOS := currentGOOS
	defer func() { currentGOOS = origGOOS }()
	currentGOOS = func() string { return "linux" }

	client := NewGitHubClient("test", "feidex", &http.Client{Transport: stubTransport{
		responses: map[string]string{
			"https://api.github.com/repos/test/feidex/releases/latest": `{
				"tag_name":"v0.2.0",
				"html_url":"https://example.test/releases/v0.2.0",
				"published_at":"2026-04-08T00:00:00Z",
				"assets":[
					{"name":"feidex-linux-amd64","browser_download_url":"https://download.test/bin"},
					{"name":"sha256sums.txt","browser_download_url":"https://download.test/sums"}
				]
			}`,
			"https://download.test/sums": "abc123  dist/feidex-linux-amd64\n",
		},
	}})
	info, err := client.LatestLinuxBinary(context.Background(), "amd64")
	if err != nil {
		t.Fatalf("LatestLinuxBinary() error = %v", err)
	}
	if info.Version != "v0.2.0" || info.ExpectedSHA256 != "abc123" {
		t.Fatalf("LatestLinuxBinary() = %+v", info)
	}
}

func TestLatestLinuxBinaryAARCH64(t *testing.T) {
	origGOOS := currentGOOS
	defer func() { currentGOOS = origGOOS }()
	currentGOOS = func() string { return "linux" }

	client := NewGitHubClient("test", "feidex", &http.Client{Transport: stubTransport{
		responses: map[string]string{
			"https://api.github.com/repos/test/feidex/releases/latest": `{
				"tag_name":"v0.3.0",
				"html_url":"https://example.test/releases/v0.3.0",
				"published_at":"2026-04-08T00:00:00Z",
				"assets":[
					{"name":"feidex-linux-aarch64","browser_download_url":"https://download.test/bin"},
					{"name":"sha256sums.txt","browser_download_url":"https://download.test/sums"}
				]
			}`,
			"https://download.test/sums": "def456  dist/feidex-linux-aarch64\n",
		},
	}})
	info, err := client.LatestLinuxBinary(context.Background(), "arm64")
	if err != nil {
		t.Fatalf("LatestLinuxBinary(arm64) error = %v", err)
	}
	if info.BinaryName != "feidex-linux-aarch64" || info.ExpectedSHA256 != "def456" {
		t.Fatalf("LatestLinuxBinary(arm64) = %+v", info)
	}
}

func TestLinuxBinaryByVersionAMD64(t *testing.T) {
	origGOOS := currentGOOS
	defer func() { currentGOOS = origGOOS }()
	currentGOOS = func() string { return "linux" }

	client := NewGitHubClient("test", "feidex", &http.Client{Transport: stubTransport{
		responses: map[string]string{
			"https://api.github.com/repos/test/feidex/releases/tags/v0.3.0": `{
				"tag_name":"v0.3.0",
				"html_url":"https://example.test/releases/v0.3.0",
				"published_at":"2026-04-08T00:00:00Z",
				"assets":[
					{"name":"feidex-linux-amd64","browser_download_url":"https://download.test/bin"},
					{"name":"sha256sums.txt","browser_download_url":"https://download.test/sums"}
				]
			}`,
			"https://download.test/sums": "abc123  dist/feidex-linux-amd64\n",
		},
	}})
	info, err := client.LinuxBinaryByVersion(context.Background(), "v0.3.0", "amd64")
	if err != nil {
		t.Fatalf("LinuxBinaryByVersion() error = %v", err)
	}
	if info.Version != "v0.3.0" || info.ExpectedSHA256 != "abc123" {
		t.Fatalf("LinuxBinaryByVersion() = %+v", info)
	}
}

func TestLatestDevLinuxBinaryAMD64(t *testing.T) {
	origGOOS := currentGOOS
	defer func() { currentGOOS = origGOOS }()
	currentGOOS = func() string { return "linux" }

	client := NewGitHubClient("test", "feidex", &http.Client{Transport: stubTransport{
		responses: map[string]string{
			"https://api.github.com/repos/test/feidex/releases/tags/dev-latest": `{
				"tag_name":"dev-latest",
				"name":"dev-20260415T080000-a1b2c3d4e5f6",
				"html_url":"https://example.test/releases/dev-latest",
				"published_at":"2026-04-15T00:00:00Z",
				"target_commitish":"a1b2c3d4e5f67890",
				"prerelease":true,
				"assets":[
					{"name":"feidex-linux-amd64","browser_download_url":"https://download.test/dev-bin"},
					{"name":"sha256sums.txt","browser_download_url":"https://download.test/dev-sums"}
				]
			}`,
			"https://download.test/dev-sums": "fedcba  dist/feidex-linux-amd64\n",
		},
	}})
	info, err := client.LatestDevLinuxBinary(context.Background(), "amd64")
	if err != nil {
		t.Fatalf("LatestDevLinuxBinary() error = %v", err)
	}
	if info.Version != "dev-20260415T080000-a1b2c3d4e5f6" || info.ReleaseTag != DevReleaseTag || info.SourceCommit != "a1b2c3d4e5f67890" || !info.Prerelease || info.ExpectedSHA256 != "fedcba" {
		t.Fatalf("LatestDevLinuxBinary() = %+v", info)
	}
	if !info.PublishedAt.Equal(time.Date(2026, time.April, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("LatestDevLinuxBinary() = %+v", info)
	}
}

func TestCurrentAssetName(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "feidex-linux-amd64"},
		{"linux", "arm64", "feidex-linux-aarch64"},
		{"darwin", "amd64", "feidex-darwin-amd64"},
		{"darwin", "arm64", "feidex-darwin-arm64"},
	}
	for _, tc := range cases {
		got, err := CurrentAssetName(tc.goos, tc.goarch)
		if err != nil || got != tc.want {
			t.Fatalf("CurrentAssetName(%s, %s) = %q, %v", tc.goos, tc.goarch, got, err)
		}
	}
	if _, err := CurrentAssetName("linux", "386"); err == nil {
		t.Fatal("expected unsupported linux architecture to fail")
	}
	if _, err := CurrentAssetName("darwin", "386"); err == nil {
		t.Fatal("expected unsupported darwin architecture to fail")
	}
	if _, err := CurrentAssetName("windows", "amd64"); err == nil {
		t.Fatal("expected unsupported platform to fail")
	}
}

type stubTransport struct {
	responses map[string]string
}

func (t stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, ok := t.responses[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("not found")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestCompareVersions(t *testing.T) {
	if got, err := CompareVersions("v0.2.0", "v0.2.1"); err != nil || got >= 0 {
		t.Fatalf("CompareVersions(v0.2.0, v0.2.1) = %d, %v", got, err)
	}
	if got, err := CompareVersions("v0.2.1", "v0.2.0"); err != nil || got <= 0 {
		t.Fatalf("CompareVersions(v0.2.1, v0.2.0) = %d, %v", got, err)
	}
	if got, err := CompareVersions("v0.2.0", "0.2.0"); err != nil || got != 0 {
		t.Fatalf("CompareVersions(v0.2.0, 0.2.0) = %d, %v", got, err)
	}
}

// --- merged from github_even_more_test.go ---

func TestReleaseAdditionalBranches(t *testing.T) {
	origGOOS := currentGOOS
	defer func() { currentGOOS = origGOOS }()
	currentGOOS = func() string { return "linux" }

	if got, err := CurrentLinuxAssetName(""); err != nil || got == "" {
		t.Fatalf("CurrentLinuxAssetName(empty) = %q, %v", got, err)
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("unsupported host arch for CurrentLinuxAssetName(empty) assertion")
	}

	client := NewGitHubClient("test", "feidex", nil)
	client.http.Transport = stubTransport{
		responses: map[string]string{
			"https://api.github.com/repos/test/feidex/releases/latest": `{
				"tag_name":"v0.2.0",
				"assets":[
					{"name":"feidex-linux-amd64","browser_download_url":"https://download.test/bin"},
					{"name":"sha256sums.txt","browser_download_url":"https://download.test/sums"}
				]
			}`,
			"https://download.test/sums": "abc other-file\n",
		},
	}
	if _, err := client.LatestLinuxBinary(context.Background(), "amd64"); err == nil || !strings.Contains(err.Error(), "missing checksum") {
		t.Fatalf("LatestLinuxBinary(missing checksum) error = %v", err)
	}

	client.http.Transport = stubTransport{responses: map[string]string{}}
	if _, err := client.fetchChecksums(context.Background(), "https://download.test/sums"); err == nil || !strings.Contains(err.Error(), "status=404") {
		t.Fatalf("fetchChecksums(404) error = %v", err)
	}

	if _, err := CompareVersions("bad", "v1.0.0"); err == nil {
		t.Fatal("expected CompareVersions invalid input to fail")
	}
	if got := compareSuffix("-rc1", "-rc1"); got != 0 {
		t.Fatalf("compareSuffix(equal) = %d, want 0", got)
	}
}

// --- merged from github_more_test.go ---

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
