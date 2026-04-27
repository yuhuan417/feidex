package release

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type errTransport struct {
	err error
}

func (t errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

func TestReleaseClientBranchCoverage(t *testing.T) {
	origGOOS := currentGOOS
	defer func() { currentGOOS = origGOOS }()
	currentGOOS = func() string { return "linux" }

	var nilClient *GitHubClient
	if _, err := nilClient.LatestLinuxBinary(context.Background(), "amd64"); err == nil || !strings.Contains(err.Error(), "nil release client") {
		t.Fatalf("nil LatestLinuxBinary() error = %v", err)
	}
	if _, err := nilClient.LatestDevLinuxBinary(context.Background(), "amd64"); err == nil || !strings.Contains(err.Error(), "nil release client") {
		t.Fatalf("nil LatestDevLinuxBinary() error = %v", err)
	}
	if _, err := nilClient.LinuxBinaryByVersion(context.Background(), "v1.0.0", "amd64"); err == nil || !strings.Contains(err.Error(), "nil release client") {
		t.Fatalf("nil LinuxBinaryByVersion() error = %v", err)
	}

	client := NewGitHubClient("test", "feidex", &http.Client{Transport: errTransport{err: errors.New("boom")}})
	if _, err := client.fetchRelease(context.Background(), "https://api.github.com/repos/test/feidex/releases/latest", "latest release"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("fetchRelease(network) error = %v", err)
	}
	if _, err := client.fetchChecksums(context.Background(), "https://download.test/sums"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("fetchChecksums(network) error = %v", err)
	}
}

func TestReleaseVersionBranches(t *testing.T) {
	origGOOS := currentGOOS
	defer func() { currentGOOS = origGOOS }()
	currentGOOS = func() string { return "linux" }

	client := NewGitHubClient("test", "feidex", nil)
	if _, err := client.LinuxBinaryByVersion(context.Background(), "", "amd64"); err == nil || !strings.Contains(err.Error(), "missing release version") {
		t.Fatalf("LinuxBinaryByVersion(empty) error = %v", err)
	}

	cases := []struct {
		a, b string
		want int
	}{
		{"v2.0.0", "v1.9.9", 1},
		{"v1.2.0", "v1.3.0", -1},
		{"v1.2.3", "v1.2.2", 1},
		{"v1.2.3-alpha.1", "v1.2.3-alpha.2", -1},
		{"v1.2.3-1", "v1.2.3-alpha", -1},
		{"v1.2.3-alpha.2", "v1.2.3-alpha.1", 1},
	}
	for _, tc := range cases {
		got, err := CompareVersions(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Fatalf("CompareVersions(%q, %q) = %d, %v, want %d", tc.a, tc.b, got, err, tc.want)
		}
	}

	if got := compareSuffix("-rc.1", "-rc.1.extra"); got >= 0 {
		t.Fatalf("compareSuffix(shorter, longer) = %d, want < 0", got)
	}
	if got := compareSuffix("-beta", "-1"); got <= 0 {
		t.Fatalf("compareSuffix(text, numeric) = %d, want > 0", got)
	}

	if parts := splitPrerelease(".-alpha..1-"); len(parts) != 2 || parts[0] != "alpha" || parts[1] != "1" {
		t.Fatalf("splitPrerelease() = %#v", parts)
	}
	if v, ok := parseNumericPrerelease("007"); !ok || v != 7 {
		t.Fatalf("parseNumericPrerelease(007) = %d, %v", v, ok)
	}
	if _, ok := parseNumericPrerelease("7a"); ok {
		t.Fatal("parseNumericPrerelease(7a) should fail")
	}
}

func TestReleaseInfoFromGitHubReleaseBranches(t *testing.T) {
	origGOOS := currentGOOS
	defer func() { currentGOOS = origGOOS }()
	currentGOOS = func() string { return "linux" }

	client := NewGitHubClient("test", "feidex", &http.Client{Transport: stubTransport{
		responses: map[string]string{
			"https://download.test/sums":     "abc123  feidex-linux-amd64\n",
			"https://download.test/bad-json": "{",
		},
	}})

	info, err := client.releaseInfoFromGitHubRelease(context.Background(), &githubRelease{
		TagName: "v0.9.0",
		Assets: []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		}{
			{Name: "feidex-linux-amd64", BrowserDownloadURL: "https://download.test/bin"},
			{Name: "sha256sums.txt", BrowserDownloadURL: "https://download.test/sums"},
		},
	}, "feidex-linux-amd64", "latest release")
	if err != nil || info.Version != "v0.9.0" {
		t.Fatalf("releaseInfoFromGitHubRelease(fallback version) = %+v, %v", info, err)
	}

	if _, err := client.releaseInfoFromGitHubRelease(context.Background(), &githubRelease{}, "feidex-linux-amd64", "latest release"); err == nil || !strings.Contains(err.Error(), "missing tag_name") {
		t.Fatalf("releaseInfoFromGitHubRelease(missing tag) error = %v", err)
	}

	badJSONClient := NewGitHubClient("test", "feidex", &http.Client{Transport: stubTransport{
		responses: map[string]string{
			"https://api.github.com/repos/test/feidex/releases/latest": "{",
		},
	}})
	if _, err := badJSONClient.fetchLatestRelease(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "EOF") {
		t.Fatalf("fetchLatestRelease(bad json) error = %v", err)
	}
}

func TestFetchChecksumsScannerError(t *testing.T) {
	client := NewGitHubClient("test", "feidex", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(errReader{}),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})
	if _, err := client.fetchChecksums(context.Background(), "https://download.test/sums"); err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("fetchChecksums(scanner error) = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
