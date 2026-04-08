package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLatestLinuxAMD64(t *testing.T) {
	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/repos/test/feidex/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		payload := `{
			"tag_name":"v0.2.0",
			"html_url":"https://example.test/releases/v0.2.0",
			"published_at":"2026-04-08T00:00:00Z",
			"assets":[
				{"name":"feidex-linux-amd64","browser_download_url":"__SERVER_URL__/download/bin"},
				{"name":"sha256sums.txt","browser_download_url":"__SERVER_URL__/download/sums"}
			]
		}`
		_, _ = w.Write([]byte(strings.ReplaceAll(payload, "__SERVER_URL__", server.URL)))
	})
	mux.HandleFunc("/download/sums", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abc123  feidex-linux-amd64\n"))
	})
	server = httptest.NewServer(mux)
	defer server.Close()

	client := NewGitHubClient("test", "feidex", server.Client())
	client.http.Transport = rewriteTransport{base: http.DefaultTransport, baseURL: server.URL}
	info, err := client.LatestLinuxAMD64(context.Background())
	if err != nil {
		t.Fatalf("LatestLinuxAMD64() error = %v", err)
	}
	if info.Version != "v0.2.0" || info.ExpectedSHA256 != "abc123" {
		t.Fatalf("LatestLinuxAMD64() = %+v", info)
	}
}

type rewriteTransport struct {
	base    http.RoundTripper
	baseURL string
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "api.github.com" {
		replaced, err := http.NewRequestWithContext(req.Context(), req.Method, t.baseURL+req.URL.Path, req.Body)
		if err != nil {
			return nil, err
		}
		replaced.Header = req.Header.Clone()
		req = replaced
	}
	return t.base.RoundTrip(req)
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
