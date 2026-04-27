package release

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

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
