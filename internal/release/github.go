package release

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"feidex/internal/buildinfo"
)

const (
	DefaultRepoOwner = "yuhuan417"
	DefaultRepoName  = "feidex"

	linuxAMD64AssetName   = "feidex-linux-amd64"
	linuxAARCH64AssetName = "feidex-linux-aarch64"
	sha256AssetName       = "sha256sums.txt"
)

type ReleaseInfo struct {
	Version        string
	HTMLURL        string
	PublishedAt    time.Time
	BinaryName     string
	BinaryURL      string
	ExpectedSHA256 string
}

type Client interface {
	LatestLinuxBinary(ctx context.Context, goarch string) (*ReleaseInfo, error)
}

type GitHubClient struct {
	owner string
	repo  string
	http  *http.Client
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func NewGitHubClient(owner, repo string, httpClient *http.Client) *GitHubClient {
	if strings.TrimSpace(owner) == "" {
		owner = DefaultRepoOwner
	}
	if strings.TrimSpace(repo) == "" {
		repo = DefaultRepoName
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &GitHubClient{
		owner: owner,
		repo:  repo,
		http:  httpClient,
	}
}

func CurrentLinuxAssetName(goarch string) (string, error) {
	switch strings.TrimSpace(goarch) {
	case "":
		return CurrentLinuxAssetName(runtime.GOARCH)
	case "amd64":
		return linuxAMD64AssetName, nil
	case "arm64":
		return linuxAARCH64AssetName, nil
	default:
		return "", fmt.Errorf("unsupported linux architecture %q", goarch)
	}
}

func (c *GitHubClient) LatestLinuxBinary(ctx context.Context, goarch string) (*ReleaseInfo, error) {
	if c == nil {
		return nil, fmt.Errorf("nil release client")
	}
	assetName, err := CurrentLinuxAssetName(goarch)
	if err != nil {
		return nil, err
	}
	release, err := c.fetchLatestRelease(ctx)
	if err != nil {
		return nil, err
	}
	info := &ReleaseInfo{
		Version: strings.TrimSpace(release.TagName),
		HTMLURL: strings.TrimSpace(release.HTMLURL),
	}
	if publishedAt := strings.TrimSpace(release.PublishedAt); publishedAt != "" {
		if ts, err := time.Parse(time.RFC3339, publishedAt); err == nil {
			info.PublishedAt = ts
		}
	}
	checksumsURL := ""
	for _, asset := range release.Assets {
		switch strings.TrimSpace(asset.Name) {
		case assetName:
			info.BinaryName = strings.TrimSpace(asset.Name)
			info.BinaryURL = strings.TrimSpace(asset.BrowserDownloadURL)
		case sha256AssetName:
			checksumsURL = strings.TrimSpace(asset.BrowserDownloadURL)
		}
	}
	if info.Version == "" {
		return nil, fmt.Errorf("latest release is missing tag_name")
	}
	if info.BinaryURL == "" || info.BinaryName == "" {
		return nil, fmt.Errorf("latest release %s is missing asset %s", info.Version, assetName)
	}
	if checksumsURL == "" {
		return nil, fmt.Errorf("latest release %s is missing asset %s", info.Version, sha256AssetName)
	}
	checksums, err := c.fetchChecksums(ctx, checksumsURL)
	if err != nil {
		return nil, err
	}
	expected := strings.TrimSpace(checksums[info.BinaryName])
	if expected == "" {
		return nil, fmt.Errorf("latest release %s is missing checksum for %s", info.Version, info.BinaryName)
	}
	info.ExpectedSHA256 = expected
	return info, nil
}

func (c *GitHubClient) fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", c.owner, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "feidex/"+buildinfo.CurrentVersion())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("github latest release request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

func (c *GitHubClient) fetchChecksums(ctx context.Context, url string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "feidex/"+buildinfo.CurrentVersion())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("github checksum request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	scanner.Buffer(make([]byte, 0, 16*1024), 1<<20)
	values := map[string]string{}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(strings.TrimSpace(fields[len(fields)-1]), "*")
		sum := strings.TrimSpace(fields[0])
		if name == "" || sum == "" {
			continue
		}
		values[name] = sum
		values[filepath.Base(name)] = sum
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

var semverRe = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:([.-][0-9A-Za-z.-]+))?$`)

type VersionInfo struct {
	Major  int
	Minor  int
	Patch  int
	Suffix string
}

func ParseVersion(raw string) (VersionInfo, error) {
	matches := semverRe.FindStringSubmatch(strings.TrimSpace(raw))
	if matches == nil {
		return VersionInfo{}, fmt.Errorf("invalid version %q", raw)
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])
	return VersionInfo{
		Major:  major,
		Minor:  minor,
		Patch:  patch,
		Suffix: strings.TrimSpace(matches[4]),
	}, nil
}

func CompareVersions(a, b string) (int, error) {
	av, err := ParseVersion(a)
	if err != nil {
		return 0, err
	}
	bv, err := ParseVersion(b)
	if err != nil {
		return 0, err
	}
	switch {
	case av.Major != bv.Major:
		if av.Major < bv.Major {
			return -1, nil
		}
		return 1, nil
	case av.Minor != bv.Minor:
		if av.Minor < bv.Minor {
			return -1, nil
		}
		return 1, nil
	case av.Patch != bv.Patch:
		if av.Patch < bv.Patch {
			return -1, nil
		}
		return 1, nil
	}
	return compareSuffix(av.Suffix, bv.Suffix), nil
}

func compareSuffix(a, b string) int {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	case a < b:
		return -1
	default:
		return 1
	}
}

func VerifySHA256(content []byte, expected string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if len(content) == 0 {
		return fmt.Errorf("downloaded content is empty")
	}
	sum := sha256.Sum256(content)
	actual := hex.EncodeToString(sum[:])
	if actual != expected {
		return fmt.Errorf("sha256 mismatch: got %s want %s", actual, expected)
	}
	return nil
}
