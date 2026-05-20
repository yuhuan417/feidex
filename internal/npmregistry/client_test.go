package npmregistry

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type stubRoundTripper func(*http.Request) (*http.Response, error)

func (f stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestLatestVersionQueriesEscapedPackageLatestEndpoint(t *testing.T) {
	client := &http.Client{Transport: stubRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %q, want GET", req.Method)
		}
		if got := req.URL.EscapedPath(); got != "/@openai%2Fcodex/latest" {
			t.Fatalf("path = %q, want escaped package latest endpoint", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"version":"1.2.3"}`)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	})}

	version, err := LatestVersion(context.Background(), client, "@openai/codex")
	if err != nil {
		t.Fatalf("LatestVersion() error = %v", err)
	}
	if version != "1.2.3" {
		t.Fatalf("LatestVersion() = %q, want 1.2.3", version)
	}
}

func TestLatestVersionRejectsMissingVersion(t *testing.T) {
	client := &http.Client{Transport: stubRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
		}, nil
	})}

	if _, err := LatestVersion(context.Background(), client, "@openai/codex"); err == nil {
		t.Fatal("LatestVersion() error = nil, want missing version error")
	}
}
