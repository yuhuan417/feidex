// Package appconfig talks to the Feishu application-management APIs that the
// vendored SDK does not wrap: it reads an application's granted scopes and
// published event subscriptions, applies configuration fixes, and publishes
// new versions. It is used by the startup self-heal flow so that every bot
// converges on the scopes and subscriptions its binary requires.
package appconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"feidex/internal/config"
)

const (
	// PatchScope gates both application config updates and publishing.
	// Without it a bot can only read its own configuration and must ask its
	// owner for the grant through an auth link.
	PatchScope = "application:application:patch"

	tokenTypeTenant = "tenant"

	codeMissingScope = 99991672
	codeUnderReview  = 210040
)

// State is a snapshot of the platform-side configuration that matters for
// self-healing: the application's granted scopes plus the subscriptions and
// scopes of the currently published version.
type State struct {
	AppScopes       []string
	OnlineVersion   string
	OnlineVersionID string
	// OnlineVersionStatus is the online version's audit status, using the
	// platform enum mirrored by AppVersionStatus* constants.
	OnlineVersionStatus int
	// UnauditVersionID is the version currently under audit, if any. Publishing
	// a change that needs review leaves the online version untouched and parks
	// the new one here.
	UnauditVersionID string
	VersionScopes    []string
	VersionEvents    []string
}

// Application version audit status, mirrored from the official SDK
// (service/application/v6 AppVersionStatus*).
const (
	AppVersionStatusUnknown    = 0 // 未知状态
	AppVersionStatusAudited    = 1 // 审核通过
	AppVersionStatusReject     = 2 // 审核拒绝
	AppVersionStatusUnderAudit = 3 // 审核中
	AppVersionStatusUnaudit    = 4 // 未提交审核
)

// HasScope reports whether the application has been granted the scope.
func (s *State) HasScope(scope string) bool {
	if s == nil {
		return false
	}
	for _, candidate := range s.AppScopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

// FixPlan describes the configuration changes needed to bring an application
// back in line with the binary's requirements.
type FixPlan struct {
	AddEvents    []string
	RemoveEvents []string
	AddScopes    []string
}

// Empty reports whether the plan contains no changes.
func (p FixPlan) Empty() bool {
	return len(p.AddEvents) == 0 && len(p.RemoveEvents) == 0 && len(p.AddScopes) == 0
}

// APIError is a structured failure from a Feishu application API.
type APIError struct {
	API  string
	Code int
	Msg  string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s failed code=%d msg=%s", e.API, e.Code, e.Msg)
}

// IsMissingScope reports whether err is the 99991672 scopes-required failure.
func IsMissingScope(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == codeMissingScope
}

// IsUnderReview reports whether err is the 210040 "application under review"
// failure.
func IsUnderReview(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == codeUnderReview
}

// AuthURL builds the console link that asks the application owner to grant
// the given scopes. The format matches the links Feishu embeds in its own
// 99991672 error messages.
func AuthURL(baseURL, appID string, scopes []string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return fmt.Sprintf("%s/app/%s/auth?q=%s&op_from=openapi&token_type=tenant",
		baseURL, strings.TrimSpace(appID), strings.Join(scopes, ","))
}

// Client is a minimal Feishu application-management API client.
type Client struct {
	baseURL    string
	appID      string
	appSecret  string
	httpClient *http.Client
}

// NewClient builds a client for one Feishu application.
func NewClient(cfg *config.FeishuConfig) *Client {
	baseURL := ""
	if cfg != nil {
		baseURL = strings.TrimSpace(cfg.OpenBaseURL())
	}
	client := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
	if cfg != nil {
		client.appID = strings.TrimSpace(cfg.AppID)
		client.appSecret = strings.TrimSpace(cfg.AppSecret)
	}
	return client
}

// FetchState reads the granted scopes and the published version's scopes and
// events. Reading requires no special scope.
func (c *Client) FetchState(ctx context.Context) (*State, error) {
	if c == nil || c.appID == "" || c.appSecret == "" {
		return nil, fmt.Errorf("appconfig: app id/secret not configured")
	}
	token, err := c.tenantToken(ctx)
	if err != nil {
		return nil, err
	}
	var appResp struct {
		Data struct {
			App struct {
				Scopes []struct {
					Scope string `json:"scope"`
				} `json:"scopes"`
				OnlineVersionID  string `json:"online_version_id"`
				UnauditVersionID string `json:"unaudit_version_id"`
			} `json:"app"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet,
		fmt.Sprintf("/open-apis/application/v6/applications/%s?lang=zh_cn", c.appID),
		token, nil, &appResp, "application.get"); err != nil {
		return nil, err
	}
	state := &State{
		OnlineVersionID:  strings.TrimSpace(appResp.Data.App.OnlineVersionID),
		UnauditVersionID: strings.TrimSpace(appResp.Data.App.UnauditVersionID),
	}
	for _, scope := range appResp.Data.App.Scopes {
		if value := strings.TrimSpace(scope.Scope); value != "" {
			state.AppScopes = append(state.AppScopes, value)
		}
	}
	if state.OnlineVersionID == "" {
		return state, nil
	}
	var versionResp struct {
		Data struct {
			AppVersion struct {
				Version string `json:"version"`
				Status  int    `json:"status"`
				Scopes  []struct {
					Scope string `json:"scope"`
				} `json:"scopes"`
				EventInfos []struct {
					EventType string `json:"event_type"`
				} `json:"event_infos"`
			} `json:"app_version"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet,
		fmt.Sprintf("/open-apis/application/v6/applications/%s/app_versions/%s?lang=zh_cn", c.appID, state.OnlineVersionID),
		token, nil, &versionResp, "application.app_version.get"); err != nil {
		return nil, err
	}
	version := versionResp.Data.AppVersion
	state.OnlineVersion = strings.TrimSpace(version.Version)
	state.OnlineVersionStatus = version.Status
	for _, scope := range version.Scopes {
		if value := strings.TrimSpace(scope.Scope); value != "" {
			state.VersionScopes = append(state.VersionScopes, value)
		}
	}
	for _, info := range version.EventInfos {
		if value := strings.TrimSpace(info.EventType); value != "" {
			state.VersionEvents = append(state.VersionEvents, value)
		}
	}
	return state, nil
}

// ApplyFix writes the plan into the application configuration. The changes
// only take effect once a new version is published.
func (c *Client) ApplyFix(ctx context.Context, plan FixPlan) error {
	if c == nil || c.appID == "" || c.appSecret == "" {
		return fmt.Errorf("appconfig: app id/secret not configured")
	}
	if plan.Empty() {
		return nil
	}
	token, err := c.tenantToken(ctx)
	if err != nil {
		return err
	}
	body := map[string]any{}
	event := map[string]any{"subscription_type": "websocket"}
	if len(plan.AddEvents) > 0 {
		event["add_events"] = plan.AddEvents
	}
	if len(plan.RemoveEvents) > 0 {
		event["remove_events"] = plan.RemoveEvents
	}
	if len(plan.AddEvents) > 0 || len(plan.RemoveEvents) > 0 {
		body["event"] = event
	}
	if len(plan.AddScopes) > 0 {
		scopes := make([]map[string]string, 0, len(plan.AddScopes))
		for _, scope := range plan.AddScopes {
			scopes = append(scopes, map[string]string{"scope_name": scope, "token_type": tokenTypeTenant})
		}
		body["scope"] = map[string]any{"add_scopes": scopes}
	}
	if len(body) == 0 {
		return nil
	}
	return c.doJSON(ctx, http.MethodPatch,
		fmt.Sprintf("/open-apis/application/v7/applications/%s/config", c.appID),
		token, body, nil, "application.config.patch")
}

// Publish submits the pending configuration as a new application version.
// The platform auto-creates the version when none is pending.
func (c *Client) Publish(ctx context.Context, remark, changelog string) (string, error) {
	if c == nil || c.appID == "" || c.appSecret == "" {
		return "", fmt.Errorf("appconfig: app id/secret not configured")
	}
	token, err := c.tenantToken(ctx)
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"remark":                 remark,
		"changelog":              changelog,
		"mobile_default_ability": "bot",
		"pc_default_ability":     "bot",
	}
	var resp struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost,
		fmt.Sprintf("/open-apis/application/v7/applications/%s/publish", c.appID),
		token, body, &resp, "application.publish"); err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Data.Version), nil
}

func (c *Client) tenantToken(ctx context.Context) (string, error) {
	body := map[string]string{"app_id": c.appID, "app_secret": c.appSecret}
	var resp struct {
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/open-apis/auth/v3/tenant_access_token/internal", "", body, &resp, "auth.tenant_access_token"); err != nil {
		return "", err
	}
	token := strings.TrimSpace(resp.TenantAccessToken)
	if token == "" {
		return "", fmt.Errorf("auth.tenant_access_token returned empty token")
	}
	return token, nil
}

func (c *Client) doJSON(ctx context.Context, method, path, token string, body any, out any, api string) error {
	if c == nil || c.baseURL == "" {
		return fmt.Errorf("appconfig: base url not configured")
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%s marshal request: %w", api, err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("%s build request: %w", api, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed: %w", api, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%s read response: %w", api, err)
	}
	var envelope struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("%s decode response (http %d): %w", api, resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || envelope.Code != 0 {
		return &APIError{API: api, Code: envelope.Code, Msg: strings.TrimSpace(envelope.Msg)}
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%s decode payload: %w", api, err)
		}
	}
	return nil
}
