package config

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type setupRoundTripper func(*http.Request) (*http.Response, error)

func (fn setupRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func withDefaultTransport(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	orig := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = orig })
}

func testHTTPResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func captureOutput(t *testing.T, fn func()) (stdout string, stderr string) {
	t.Helper()

	origStdout := os.Stdout
	origStderr := os.Stderr
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stdout): %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stderr): %v", err)
	}
	os.Stdout = outW
	os.Stderr = errW

	var outBuf, errBuf bytes.Buffer
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(&outBuf, outR)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(&errBuf, errR)
		done <- struct{}{}
	}()

	fn()

	_ = outW.Close()
	_ = errW.Close()
	<-done
	<-done
	return outBuf.String(), errBuf.String()
}

func TestParsePairAndLoadOrCreateConfig(t *testing.T) {
	appID, secret, err := parsePair(" app-id : app-secret ")
	if err != nil || appID != "app-id" || secret != "app-secret" {
		t.Fatalf("parsePair() = %q, %q, %v", appID, secret, err)
	}
	if _, _, err := parsePair("missing-colon"); err == nil {
		t.Fatal("expected malformed pair to fail")
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	cfg, err := loadOrCreateConfig(cfgPath, "repo")
	if err != nil {
		t.Fatalf("loadOrCreateConfig(new) error = %v", err)
	}
	if cfg.Workspaces[0].ID != "repo" || cfg.Workspaces[0].Name != "repo" {
		t.Fatalf("workspace override not applied: %+v", cfg.Workspaces[0])
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected config file to be created: %v", err)
	}

	cfg.Feishu.AppID = "saved"
	if err := Save(cfgPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := loadOrCreateConfig(cfgPath, "")
	if err != nil {
		t.Fatalf("loadOrCreateConfig(existing) error = %v", err)
	}
	if loaded.Feishu.AppID != "saved" {
		t.Fatalf("loaded config = %+v, want saved values", loaded.Feishu)
	}
}

func TestValidateFeishuCredentialsAndRegistrationCall(t *testing.T) {
	withDefaultTransport(t, setupRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "open.feishu.cn" {
			return testHTTPResponse(`{"code":0,"tenant_access_token":"token"}`), nil
		}
		return testHTTPResponse(`{"ok":true}`), nil
	}))
	if err := validateFeishuCredentials("app", "secret"); err != nil {
		t.Fatalf("validateFeishuCredentials(success) error = %v", err)
	}

	withDefaultTransport(t, setupRoundTripper(func(req *http.Request) (*http.Response, error) {
		return testHTTPResponse(`{"code":999,"msg":"bad creds"}`), nil
	}))
	if err := validateFeishuCredentials("app", "secret"); err == nil {
		t.Fatal("expected invalid credentials to fail")
	}

	client := &http.Client{Transport: setupRoundTripper(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		values, _ := url.ParseQuery(string(body))
		if values.Get("action") != "poll" || values.Get("device_code") != "device-1" {
			t.Fatalf("unexpected registration request body: %q", string(body))
		}
		return testHTTPResponse(`{"client_id":"cli","client_secret":"secret"}`), nil
	})}
	var poll registrationPollResponse
	if err := registrationCall(client, "poll", map[string]string{"device_code": "device-1"}, &poll); err != nil {
		t.Fatalf("registrationCall() error = %v", err)
	}
	if poll.ClientID != "cli" || poll.ClientSecret != "secret" {
		t.Fatalf("registrationCall() = %+v, want parsed credentials", poll)
	}
}

func TestRunRegistrationFlowAndQRCodeHelpers(t *testing.T) {
	qrImage := filepath.Join(t.TempDir(), "qr.png")
	withDefaultTransport(t, setupRoundTripper(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		values, _ := url.ParseQuery(string(body))
		switch values.Get("action") {
		case "init":
			return testHTTPResponse(`{"supported_auth_methods":["client_secret"]}`), nil
		case "begin":
			return testHTTPResponse(`{"device_code":"dev-1","verification_uri_complete":"https://example.test/scan","interval":0,"expire_in":60}`), nil
		case "poll":
			return testHTTPResponse(`{"client_id":"app-id","client_secret":"app-secret"}`), nil
		default:
			t.Fatalf("unexpected action: %q", values.Get("action"))
			return nil, nil
		}
	}))

	stdout, stderr := captureOutput(t, func() {
		appID, secret, err := runRegistrationFlow(100*time.Millisecond, qrImage)
		if err != nil {
			t.Fatalf("runRegistrationFlow(success) error = %v", err)
		}
		if appID != "app-id" || secret != "app-secret" {
			t.Fatalf("runRegistrationFlow(success) = %q, %q", appID, secret)
		}
	})
	if stderr != "" || !strings.Contains(stdout, "二维码已保存到") {
		t.Fatalf("runRegistrationFlow output = %q / %q", stdout, stderr)
	}
	if info, err := os.Stat(qrImage); err != nil || info.Size() == 0 {
		t.Fatalf("expected qr image to be written, err=%v info=%+v", err, info)
	}
}

func TestRunRegistrationFlowErrorsAndSetupFeishu(t *testing.T) {
	withDefaultTransport(t, setupRoundTripper(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		values, _ := url.ParseQuery(string(body))
		switch values.Get("action") {
		case "init":
			return testHTTPResponse(`{"supported_auth_methods":["client_secret"]}`), nil
		case "begin":
			return testHTTPResponse(`{"device_code":"dev-1","verification_uri_complete":"https://example.test/scan","interval":0,"expire_in":60}`), nil
		case "poll":
			return testHTTPResponse(`{"error":"access_denied","error_description":"nope"}`), nil
		default:
			return testHTTPResponse(`{}`), nil
		}
	}))
	if _, _, err := runRegistrationFlow(20*time.Millisecond, ""); err == nil || !strings.Contains(err.Error(), "authorization denied") {
		t.Fatalf("runRegistrationFlow(access denied) error = %v, want access denied", err)
	}

	withDefaultTransport(t, setupRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "open.feishu.cn" {
			return testHTTPResponse(`{"code":0,"tenant_access_token":"token"}`), nil
		}
		return testHTTPResponse(`{}`), nil
	}))

	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	stdout, _ := captureOutput(t, func() {
		if err := SetupFeishu(FeishuSetupBind, FeishuSetupOptions{
			ConfigPath: cfgPath,
			AppPair:    "bind-id:bind-secret",
			Workspace:  "repo",
		}); err != nil {
			t.Fatalf("SetupFeishu(bind) error = %v", err)
		}
	})
	if !strings.Contains(stdout, "Feishu credentials saved") {
		t.Fatalf("SetupFeishu(bind) output = %q, want save message", stdout)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(saved config) error = %v", err)
	}
	if cfg.Feishu.AppID != "bind-id" || cfg.Feishu.AppSecret != "bind-secret" {
		t.Fatalf("saved credentials = %+v, want bound values", cfg.Feishu)
	}

	if err := SetupFeishu(FeishuSetupNew, FeishuSetupOptions{AppID: "existing"}); err == nil {
		t.Fatal("expected new mode with existing credentials to fail")
	}
	if err := SetupFeishu(FeishuSetupBind, FeishuSetupOptions{}); err == nil {
		t.Fatal("expected bind mode without credentials to fail")
	}
	if err := SetupFeishu(FeishuSetupMode("bad"), FeishuSetupOptions{}); err == nil {
		t.Fatal("expected unsupported mode to fail")
	}
}
