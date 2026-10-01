package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/scene/registration"
	qrterminal "github.com/mdp/qrterminal/v3"
	"rsc.io/qr"
)

const accountsBaseURL = "https://accounts.feishu.cn"

// defaultRegistrationTimeout bounds how long the QR self-registration flow
// waits for the user to scan and approve.
const defaultRegistrationTimeout = 10 * time.Minute

type FeishuSetupMode string

const (
	FeishuSetupAuto FeishuSetupMode = "auto"
	FeishuSetupNew  FeishuSetupMode = "new"
	FeishuSetupBind FeishuSetupMode = "bind"
)

type FeishuSetupOptions struct {
	ConfigPath string
	Workspace  string
	AppPair    string
	AppID      string
	AppSecret  string
	Domain     string
	Timeout    time.Duration
	QRImage    string
	FrontendID string
	Backend    string
}

type registrationInitResponse struct {
	SupportedAuthMethods []string `json:"supported_auth_methods"`
	Error                string   `json:"error"`
	ErrorDescription     string   `json:"error_description"`
}

type tenantTokenResponse struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
}

func SetupFeishu(mode FeishuSetupMode, opts FeishuSetupOptions) error {
	if opts.Timeout <= 0 {
		opts.Timeout = defaultRegistrationTimeout
	}
	cfgPath := opts.ConfigPath
	if cfgPath == "" {
		cfgPath = "config.toml"
	}
	cfg, err := loadOrCreateConfig(cfgPath, opts.Workspace)
	if err != nil {
		return err
	}

	switch mode {
	case FeishuSetupAuto:
		if strings.TrimSpace(opts.AppPair) != "" || (strings.TrimSpace(opts.AppID) != "" && strings.TrimSpace(opts.AppSecret) != "") {
			mode = FeishuSetupBind
		} else {
			mode = FeishuSetupNew
		}
	}

	domain, err := normalizeFeishuDomain(opts.Domain)
	if err != nil {
		return err
	}

	appID := strings.TrimSpace(opts.AppID)
	appSecret := strings.TrimSpace(opts.AppSecret)
	if strings.TrimSpace(opts.AppPair) != "" {
		appID, appSecret, err = parsePair(opts.AppPair)
		if err != nil {
			return err
		}
	}

	switch mode {
	case FeishuSetupBind:
		if appID == "" || appSecret == "" {
			return errors.New("bind mode requires --app or --app-id/--app-secret")
		}
		if err := validateFeishuCredentials(appID, appSecret, domain); err != nil {
			return err
		}
	case FeishuSetupNew:
		if appID != "" || appSecret != "" {
			return errors.New("new mode does not accept existing credentials")
		}
		if domain == FeishuDomainLark {
			return errors.New("new mode (QR self-registration) is only supported for the feishu domain; for lark, create the app at open.larksuite.com and use `feishu bind --domain lark`")
		}
		appID, appSecret, err = runRegistrationFlow(opts.Timeout, opts.QRImage)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported mode %q", mode)
	}

	frontendID := strings.TrimSpace(opts.FrontendID)
	if frontendID != "" {
		if err := saveToFrontend(cfg, frontendID, appID, appSecret, domain, strings.TrimSpace(opts.Backend)); err != nil {
			return err
		}
	} else {
		cfg.Feishu.AppID = appID
		cfg.Feishu.AppSecret = appSecret
		cfg.Feishu.Domain = domain
	}
	if err := Save(cfgPath, cfg); err != nil {
		return err
	}
	if frontendID != "" {
		fmt.Printf("Feishu credentials saved to %s (frontend %q)\n", cfgPath, frontendID)
	} else {
		fmt.Printf("Feishu credentials saved to %s\n", cfgPath)
	}
	fmt.Printf("App ID: %s\n", appID)
	return nil
}

func saveToFrontend(cfg *Config, id, appID, appSecret, domain, backend string) error {
	if strings.Contains(id, ":") {
		return fmt.Errorf("frontend id %q must not contain ':'", id)
	}
	idx := -1
	for i := range cfg.Frontends {
		if cfg.Frontends[i].ID == id {
			idx = i
			break
		}
	}
	if idx >= 0 {
		cfg.Frontends[idx].AppID = appID
		cfg.Frontends[idx].AppSecret = appSecret
		cfg.Frontends[idx].Domain = domain
		if backend != "" {
			cfg.Frontends[idx].Backend = backend
		}
		return nil
	}
	// Migrate top-level feishu section to a [[frontend]] entry when the first
	// named frontend is added, so the config stays in one consistent form.
	if cfg.Feishu.AppID != "" && len(cfg.Frontends) == 0 {
		cfg.Frontends = append(cfg.Frontends, FrontendConfig{
			ID:           DefaultFrontendID,
			FeishuConfig: cfg.Feishu,
		})
		cfg.Feishu = FeishuConfig{}
	}
	fc := FrontendConfig{ID: id}
	fc.AppID = appID
	fc.AppSecret = appSecret
	fc.Domain = domain
	if backend != "" {
		fc.Backend = backend
	}
	cfg.Frontends = append(cfg.Frontends, fc)
	return nil
}

func loadOrCreateConfig(path, workspaceID string) (*Config, error) {
	if _, err := os.Stat(path); err == nil {
		return Load(path)
	}
	cfg := Default()
	if workspaceID != "" {
		cfg.Workspaces[0].ID = workspaceID
		cfg.Workspaces[0].Name = workspaceID
	}
	if cwd, err := os.Getwd(); err == nil {
		cfg.Workspaces[0].Cwd = cwd
	}
	cfg.DataDir = filepath.Join(filepath.Dir(path), ".feidex-data")
	return cfg, Save(path, cfg)
}

func validateFeishuCredentials(appID, appSecret, domain string) error {
	base := feishuOpenBaseURL
	if domain == FeishuDomainLark {
		base = larkOpenBaseURL
	}
	payload, _ := json.Marshal(map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	})
	req, err := http.NewRequest(http.MethodPost, base+"/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var tokenResp tenantTokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tokenResp); err != nil {
		return err
	}
	if tokenResp.Code != 0 || tokenResp.TenantAccessToken == "" {
		return fmt.Errorf("credential validation failed: code=%d msg=%s", tokenResp.Code, tokenResp.Msg)
	}
	return nil
}

// runRegistrationFlow runs the QR self-registration flow.
//
// The flow is delegated to the SDK (scene/registration). It performs the same
// device-code dance this file used to hand-roll against the same accounts host
// — its begin request was verified to send identical parameters
// (archetype=PersonalAgent, auth_method=client_secret, request_user_info=open_id)
// — and additionally switches to the Lark accounts domain by itself when the
// scanning user's tenant turns out to be Lark.
//
// QR rendering stays here: the SDK hands back only the verification URL, it
// does not encode or draw a QR image.
func runRegistrationFlow(timeout time.Duration, qrImagePath string) (string, string, error) {
	if timeout <= 0 {
		timeout = defaultRegistrationTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := registration.RegisterApp(ctx, &registration.Options{
		Domain: accountsBaseURL,
		OnQRCode: func(info *registration.QRCodeInfo) {
			renderRegistrationQR(info, qrImagePath)
		},
	})
	if err != nil {
		// Map the SDK's typed errors back onto the wording this command has
		// always shown. The SDK collapses two different situations into
		// ExpiredError — the server reporting the QR session expired, and our
		// own context deadline firing while waiting between polls — so those are
		// told apart by inspecting our context rather than the error type.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", "", errors.New("timed out waiting for Feishu onboarding result")
		}
		var denied *registration.AccessDeniedError
		if errors.As(err, &denied) {
			return "", "", errors.New("authorization denied by user")
		}
		var expired *registration.ExpiredError
		if errors.As(err, &expired) {
			return "", "", errors.New("QR onboarding session expired")
		}
		return "", "", err
	}
	if result == nil || strings.TrimSpace(result.ClientID) == "" || strings.TrimSpace(result.ClientSecret) == "" {
		return "", "", errors.New("registration flow returned incomplete credentials")
	}
	return result.ClientID, result.ClientSecret, nil
}

// renderRegistrationQR prints the verification URL as a scannable QR code and
// optionally saves it as a PNG.
func renderRegistrationQR(info *registration.QRCodeInfo, qrImagePath string) {
	if info == nil {
		return
	}
	content := strings.TrimSpace(info.URL)
	if content == "" {
		return
	}
	fmt.Println("请使用飞书手机客户端扫码完成应用创建与授权：")
	fmt.Printf("URL: %s\n\n", content)
	printQRCode(content)
	if qrImagePath == "" {
		return
	}
	if err := saveQRCode(content, qrImagePath); err != nil {
		fmt.Fprintf(os.Stderr, "保存二维码失败: %v\n", err)
		return
	}
	fmt.Printf("二维码已保存到 %s\n", qrImagePath)
}

// registrationCall posts a single action to the app-registration endpoint.
//
// Retained deliberately, not dead weight: the SDK's RegisterApp goes straight
// to "begin" and skips the "init" handshake this flow used to perform first. If
// that turns out to matter in production, the fallback is to call
//
//	registrationCall(client, "init", nil, &registrationInitResponse{})
//
// before RegisterApp. Keep it until the SDK-driven flow has been exercised
// against a real tenant.
func registrationCall(client *http.Client, action string, params map[string]string, out any) error {
	form := url.Values{}
	form.Set("action", action)
	for k, v := range params {
		form.Set(k, v)
	}
	req, err := http.NewRequest(http.MethodPost, accountsBaseURL+"/oauth/v1/app/registration", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("registration request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}

func parsePair(v string) (string, string, error) {
	idx := strings.Index(v, ":")
	if idx <= 0 || idx >= len(v)-1 {
		return "", "", errors.New("credential pair must be app_id:app_secret")
	}
	return strings.TrimSpace(v[:idx]), strings.TrimSpace(v[idx+1:]), nil
}

func printQRCode(content string) {
	qrterminal.GenerateWithConfig(content, qrterminal.Config{
		Level:      qrterminal.M,
		Writer:     os.Stdout,
		HalfBlocks: false,
		BlackChar:  "██",
		WhiteChar:  "  ",
		QuietZone:  2,
	})
	fmt.Println()
}

func saveQRCode(content, path string) error {
	code, err := qr.Encode(content, qr.M)
	if err != nil {
		return err
	}
	code.Scale = 8
	return os.WriteFile(path, code.PNG(), 0o644)
}
