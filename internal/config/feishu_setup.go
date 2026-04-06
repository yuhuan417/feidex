package config

import (
	"bytes"
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

	qrterminal "github.com/mdp/qrterminal/v3"
	"rsc.io/qr"
)

const (
	accountsBaseURL = "https://accounts.feishu.cn"
	openBaseURL     = "https://open.feishu.cn"
)

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
	Timeout    time.Duration
	QRImage    string
}

type registrationInitResponse struct {
	SupportedAuthMethods []string `json:"supported_auth_methods"`
	Error                string   `json:"error"`
	ErrorDescription     string   `json:"error_description"`
}

type registrationBeginResponse struct {
	DeviceCode              string `json:"device_code"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval                int    `json:"interval"`
	ExpireIn                int    `json:"expire_in"`
	Error                   string `json:"error"`
	ErrorDescription        string `json:"error_description"`
}

type registrationPollResponse struct {
	ClientID         string `json:"client_id"`
	ClientSecret     string `json:"client_secret"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type tenantTokenResponse struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
}

func SetupFeishu(mode FeishuSetupMode, opts FeishuSetupOptions) error {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Minute
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
		if err := validateFeishuCredentials(appID, appSecret); err != nil {
			return err
		}
	case FeishuSetupNew:
		if appID != "" || appSecret != "" {
			return errors.New("new mode does not accept existing credentials")
		}
		appID, appSecret, err = runRegistrationFlow(opts.Timeout, opts.QRImage)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported mode %q", mode)
	}

	cfg.Feishu.AppID = appID
	cfg.Feishu.AppSecret = appSecret
	if err := Save(cfgPath, cfg); err != nil {
		return err
	}
	fmt.Printf("Feishu credentials saved to %s\n", cfgPath)
	fmt.Printf("App ID: %s\n", appID)
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

func validateFeishuCredentials(appID, appSecret string) error {
	payload, _ := json.Marshal(map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	})
	req, err := http.NewRequest(http.MethodPost, openBaseURL+"/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(payload))
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

func runRegistrationFlow(timeout time.Duration, qrImagePath string) (string, string, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	var initResp registrationInitResponse
	if err := registrationCall(client, "init", nil, &initResp); err != nil {
		return "", "", err
	}
	if initResp.Error != "" {
		return "", "", fmt.Errorf("%s: %s", initResp.Error, initResp.ErrorDescription)
	}

	var beginResp registrationBeginResponse
	if err := registrationCall(client, "begin", map[string]string{
		"archetype":         "PersonalAgent",
		"auth_method":       "client_secret",
		"request_user_info": "open_id",
	}, &beginResp); err != nil {
		return "", "", err
	}
	if beginResp.Error != "" {
		return "", "", fmt.Errorf("%s: %s", beginResp.Error, beginResp.ErrorDescription)
	}
	if beginResp.VerificationURIComplete == "" || beginResp.DeviceCode == "" {
		return "", "", errors.New("registration flow returned incomplete QR response")
	}

	fmt.Println("请使用飞书手机客户端扫码完成应用创建与授权：")
	fmt.Printf("URL: %s\n\n", beginResp.VerificationURIComplete)
	printQRCode(beginResp.VerificationURIComplete)
	if qrImagePath != "" {
		if err := saveQRCode(beginResp.VerificationURIComplete, qrImagePath); err != nil {
			fmt.Fprintf(os.Stderr, "保存二维码失败: %v\n", err)
		} else {
			fmt.Printf("二维码已保存到 %s\n", qrImagePath)
		}
	}

	interval := time.Duration(beginResp.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var pollResp registrationPollResponse
		if err := registrationCall(client, "poll", map[string]string{"device_code": beginResp.DeviceCode}, &pollResp); err != nil {
			return "", "", err
		}
		if pollResp.ClientID != "" && pollResp.ClientSecret != "" {
			return pollResp.ClientID, pollResp.ClientSecret, nil
		}
		switch pollResp.Error {
		case "", "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "access_denied":
			return "", "", errors.New("authorization denied by user")
		case "expired_token":
			return "", "", errors.New("QR onboarding session expired")
		default:
			if pollResp.Error != "" {
				return "", "", fmt.Errorf("%s: %s", pollResp.Error, pollResp.ErrorDescription)
			}
		}
		time.Sleep(interval)
	}
	return "", "", errors.New("timed out waiting for Feishu onboarding result")
}

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
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
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
