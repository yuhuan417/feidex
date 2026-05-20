package npmregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultRegistryURL = "https://registry.npmjs.org"

// HTTPDoer is the subset of *http.Client used by LatestVersion.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// LatestVersion returns the version tagged "latest" for an npm package.
func LatestVersion(ctx context.Context, client HTTPDoer, packageName, userAgent string) (string, error) {
	packageName = strings.TrimSpace(packageName)
	if packageName == "" {
		return "", fmt.Errorf("missing package name")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	endpoint := strings.TrimRight(defaultRegistryURL, "/") + "/" + url.PathEscape(packageName) + "/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	if userAgent = strings.TrimSpace(userAgent); userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return "", err
	}
	version := strings.TrimSpace(payload.Version)
	if version == "" {
		return "", fmt.Errorf("registry response missing version")
	}
	return version, nil
}
