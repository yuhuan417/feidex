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
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"feidex/internal/buildinfo"
)

const (
	DefaultRepoOwner = "yuhuan417"
	DefaultRepoName  = "feidex"
	DevReleaseTag    = "dev-latest"

	linuxAMD64AssetName   = "feidex-linux-amd64"
	linuxAARCH64AssetName = "feidex-linux-aarch64"
	sha256AssetName       = "sha256sums.txt"
)

type ReleaseInfo struct {
	Version        string
	ReleaseTag     string
	HTMLURL        string
	PublishedAt    time.Time
	SourceCommit   string
	Prerelease     bool
	BinaryName     string
	BinaryURL      string
	ExpectedSHA256 string
}

type Client interface {
	LatestLinuxBinary(ctx context.Context, goarch string) (*ReleaseInfo, error)
	LatestDevLinuxBinary(ctx context.Context, goarch string) (*ReleaseInfo, error)
	LinuxBinaryByVersion(ctx context.Context, version, goarch string) (*ReleaseInfo, error)
}

type GitHubClient struct {
	owner string
	repo  string
	http  *http.Client
}

type githubRelease struct {
	TagName        string `json:"tag_name"`
	Name           string `json:"name"`
	HTMLURL        string `json:"html_url"`
	PublishedAt    string `json:"published_at"`
	TargetCommitish string `json:"target_commitish"`
	Prerelease     bool   `json:"prerelease"`
	Assets         []struct {
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
	return c.releaseInfoFromGitHubRelease(ctx, release, assetName, "latest release")
}

func (c *GitHubClient) LatestDevLinuxBinary(ctx context.Context, goarch string) (*ReleaseInfo, error) {
	if c == nil {
		return nil, fmt.Errorf("nil release client")
	}
	assetName, err := CurrentLinuxAssetName(goarch)
	if err != nil {
		return nil, err
	}
	release, err := c.fetchReleaseByTag(ctx, DevReleaseTag)
	if err != nil {
		return nil, err
	}
	return c.releaseInfoFromGitHubRelease(ctx, release, assetName, "dev release "+DevReleaseTag)
}

func (c *GitHubClient) LinuxBinaryByVersion(ctx context.Context, version, goarch string) (*ReleaseInfo, error) {
	if c == nil {
		return nil, fmt.Errorf("nil release client")
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return nil, fmt.Errorf("missing release version")
	}
	assetName, err := CurrentLinuxAssetName(goarch)
	if err != nil {
		return nil, err
	}
	release, err := c.fetchReleaseByTag(ctx, version)
	if err != nil {
		return nil, err
	}
	return c.releaseInfoFromGitHubRelease(ctx, release, assetName, "release "+version)
}

func (c *GitHubClient) releaseInfoFromGitHubRelease(ctx context.Context, release *githubRelease, assetName, releaseLabel string) (*ReleaseInfo, error) {
	tagName := strings.TrimSpace(release.TagName)
	version := strings.TrimSpace(release.Name)
	if version == "" {
		version = tagName
	}
	info := &ReleaseInfo{
		Version:      version,
		ReleaseTag:   tagName,
		HTMLURL:      strings.TrimSpace(release.HTMLURL),
		SourceCommit: strings.TrimSpace(release.TargetCommitish),
		Prerelease:   release.Prerelease,
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
	if info.ReleaseTag == "" {
		return nil, fmt.Errorf("%s is missing tag_name", releaseLabel)
	}
	if info.Version == "" {
		info.Version = info.ReleaseTag
	}
	if info.BinaryURL == "" || info.BinaryName == "" {
		return nil, fmt.Errorf("%s is missing asset %s", releaseLabel, assetName)
	}
	if checksumsURL == "" {
		return nil, fmt.Errorf("%s is missing asset %s", releaseLabel, sha256AssetName)
	}
	checksums, err := c.fetchChecksums(ctx, checksumsURL)
	if err != nil {
		return nil, err
	}
	expected := strings.TrimSpace(checksums[info.BinaryName])
	if expected == "" {
		return nil, fmt.Errorf("%s is missing checksum for %s", releaseLabel, info.BinaryName)
	}
	info.ExpectedSHA256 = expected
	return info, nil
}

func (c *GitHubClient) fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", c.owner, c.repo)
	return c.fetchRelease(ctx, url, "latest release")
}

func (c *GitHubClient) fetchReleaseByTag(ctx context.Context, tag string) (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", c.owner, c.repo, url.PathEscape(strings.TrimSpace(tag)))
	return c.fetchRelease(ctx, url, fmt.Sprintf("release %s", strings.TrimSpace(tag)))
}

func (c *GitHubClient) fetchRelease(ctx context.Context, url, label string) (*githubRelease, error) {
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
		return nil, fmt.Errorf("github %s request failed: status=%d body=%s", label, resp.StatusCode, strings.TrimSpace(string(body)))
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
	}

	partsA := splitPrerelease(a)
	partsB := splitPrerelease(b)
	limit := min(len(partsA), len(partsB))
	for i := 0; i < limit; i++ {
		partA := partsA[i]
		partB := partsB[i]
		intA, okA := parseNumericPrerelease(partA)
		intB, okB := parseNumericPrerelease(partB)
		switch {
		case okA && okB:
			if intA < intB {
				return -1
			}
			if intA > intB {
				return 1
			}
		case okA && !okB:
			return -1
		case !okA && okB:
			return 1
		case partA < partB:
			return -1
		case partA > partB:
			return 1
		}
	}
	switch {
	case len(partsA) < len(partsB):
		return -1
	case len(partsA) > len(partsB):
		return 1
	default:
		return 0
	}
}

func splitPrerelease(value string) []string {
	value = strings.TrimSpace(strings.TrimLeft(value, ".-"))
	if value == "" {
		return nil
	}
	return slices.DeleteFunc(strings.FieldsFunc(value, func(r rune) bool { return r == '.' || r == '-' }), func(part string) bool {
		return strings.TrimSpace(part) == ""
	})
}

func parseNumericPrerelease(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	v, err := strconv.Atoi(value)
	return v, err == nil
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
