package config

import (
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSetupFeishuAutoModes(t *testing.T) {
	withDefaultTransport(t, setupRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "open.feishu.cn" {
			return testHTTPResponse(`{"code":0,"tenant_access_token":"token"}`), nil
		}
		body, _ := io.ReadAll(req.Body)
		values, _ := url.ParseQuery(string(body))
		switch values.Get("action") {
		case "init":
			return testHTTPResponse(`{"supported_auth_methods":["client_secret"]}`), nil
		case "begin":
			return testHTTPResponse(`{"device_code":"dev-1","verification_uri_complete":"https://example.test/scan","interval":0,"expire_in":60}`), nil
		case "poll":
			return testHTTPResponse(`{"client_id":"new-id","client_secret":"new-secret"}`), nil
		default:
			return testHTTPResponse(`{}`), nil
		}
	}))

	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := SetupFeishu(FeishuSetupAuto, FeishuSetupOptions{ConfigPath: cfgPath, AppPair: "bind-id:bind-secret"}); err != nil {
		t.Fatalf("SetupFeishu(auto bind) error = %v", err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(auto bind) error = %v", err)
	}
	if cfg.Feishu.AppID != "bind-id" {
		t.Fatalf("auto bind app id = %q, want bind-id", cfg.Feishu.AppID)
	}

	cfgPath = filepath.Join(t.TempDir(), "config.toml")
	if err := SetupFeishu(FeishuSetupAuto, FeishuSetupOptions{ConfigPath: cfgPath, Timeout: time.Second}); err != nil {
		t.Fatalf("SetupFeishu(auto new) error = %v", err)
	}
	cfg, err = Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(auto new) error = %v", err)
	}
	if cfg.Feishu.AppID != "new-id" {
		t.Fatalf("auto new app id = %q, want new-id", cfg.Feishu.AppID)
	}
}

func TestRunRegistrationFlowTimeoutAndExpiry(t *testing.T) {
	withDefaultTransport(t, setupRoundTripper(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		values, _ := url.ParseQuery(string(body))
		switch values.Get("action") {
		case "init":
			return testHTTPResponse(`{"supported_auth_methods":["client_secret"]}`), nil
		case "begin":
			return testHTTPResponse(`{"device_code":"dev-1","verification_uri_complete":"https://example.test/scan","interval":0,"expire_in":60}`), nil
		case "poll":
			return testHTTPResponse(`{"error":"slow_down"}`), nil
		default:
			return testHTTPResponse(`{}`), nil
		}
	}))
	if _, _, err := runRegistrationFlow(10*time.Millisecond, ""); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("runRegistrationFlow(timeout) error = %v", err)
	}

	withDefaultTransport(t, setupRoundTripper(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		values, _ := url.ParseQuery(string(body))
		switch values.Get("action") {
		case "init":
			return testHTTPResponse(`{"supported_auth_methods":["client_secret"]}`), nil
		case "begin":
			return testHTTPResponse(`{"device_code":"dev-1","verification_uri_complete":"https://example.test/scan","interval":0,"expire_in":60}`), nil
		case "poll":
			return testHTTPResponse(`{"error":"expired_token"}`), nil
		default:
			return testHTTPResponse(`{}`), nil
		}
	}))
	if _, _, err := runRegistrationFlow(time.Second, ""); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("runRegistrationFlow(expired) error = %v", err)
	}
}

func TestRegistrationCallStatusError(t *testing.T) {
	client := &http.Client{Transport: setupRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 500,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"bad"}`)),
			Request:    req,
		}, nil
	})}
	var poll registrationPollResponse
	if err := registrationCall(client, "poll", map[string]string{"device_code": "device-1"}, &poll); err == nil || !strings.Contains(err.Error(), "status=500") {
		t.Fatalf("registrationCall(status error) = %v, want status error", err)
	}
}
