package feishu

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"feidex/internal/config"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

const feishuTenantAccessTokenInvalidCode = 99991663

var sharedFeishuTokenCache = newResettableLarkTokenCache()

type resettableLarkTokenCache struct {
	mu     sync.RWMutex
	values map[string]cachedLarkToken
}

type cachedLarkToken struct {
	value    string
	expireAt time.Time
}

func newResettableLarkTokenCache() *resettableLarkTokenCache {
	return &resettableLarkTokenCache{values: map[string]cachedLarkToken{}}
}

func (c *resettableLarkTokenCache) Get(_ context.Context, key string) (string, error) {
	if c == nil {
		return "", nil
	}
	key = strings.TrimSpace(key)
	c.mu.RLock()
	entry, ok := c.values[key]
	c.mu.RUnlock()
	if !ok || entry.value == "" || !entry.expireAt.After(time.Now()) {
		return "", nil
	}
	return entry.value, nil
}

func (c *resettableLarkTokenCache) Set(_ context.Context, key, value string, ttl time.Duration) error {
	if c == nil {
		return nil
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if value == "" || ttl <= 0 {
		delete(c.values, key)
		return nil
	}
	c.values[key] = cachedLarkToken{
		value:    value,
		expireAt: time.Now().Add(ttl),
	}
	return nil
}

func (c *resettableLarkTokenCache) clearTenantAccessTokens(appID string) int {
	if c == nil {
		return 0
	}
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return 0
	}
	prefix := "tenant_access_token-" + appID + "-"
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for key := range c.values {
		if strings.HasPrefix(key, prefix) {
			delete(c.values, key)
			count++
		}
	}
	return count
}

func newFeishuLarkClient(cfg config.FeishuConfig) *lark.Client {
	return lark.NewClient(cfg.AppID, cfg.AppSecret, lark.WithTokenCache(sharedFeishuTokenCache))
}

func (a *Adapter) currentClient() *lark.Client {
	if a == nil {
		return nil
	}
	a.clientMu.RLock()
	defer a.clientMu.RUnlock()
	return a.client
}

func (a *Adapter) refreshClientAfterInvalidTenantToken(api string) bool {
	if a == nil {
		return false
	}
	appID := strings.TrimSpace(a.cfg.AppID)
	cleared := sharedFeishuTokenCache.clearTenantAccessTokens(appID)

	rebuilt := false
	a.clientMu.Lock()
	if a.clientFactory != nil {
		a.client = a.clientFactory()
		rebuilt = a.client != nil
	}
	a.clientMu.Unlock()

	slog.Warn("feishu tenant access token invalid; refreshed client",
		"api", strings.TrimSpace(api),
		"app_id", appID,
		"cleared_tokens", cleared,
		"rebuilt_client", rebuilt,
	)
	return cleared > 0 || rebuilt
}

func withFeishuTenantTokenRefreshRetry[T any](ctx context.Context, a *Adapter, api string, call func(*lark.Client) (T, error)) (T, error) {
	var zero T
	if a == nil {
		return zero, fmt.Errorf("feishu adapter not initialized")
	}
	client := a.currentClient()
	if client == nil {
		return zero, fmt.Errorf("feishu adapter not initialized")
	}
	resp, err := call(client)
	if !isFeishuTenantAccessTokenInvalid(resp, err) || ctx.Err() != nil {
		return resp, err
	}
	if !a.refreshClientAfterInvalidTenantToken(api) {
		return resp, err
	}
	client = a.currentClient()
	if client == nil {
		return resp, err
	}
	return call(client)
}

func isFeishuTenantAccessTokenInvalid(resp any, err error) bool {
	if err != nil {
		var codeErr larkcore.CodeError
		if errors.As(err, &codeErr) && codeErr.Code == feishuTenantAccessTokenInvalidCode {
			return true
		}
	}
	code, _, ok := feishuResponseCodeAndMessage(resp)
	return ok && code == feishuTenantAccessTokenInvalidCode
}

func feishuResponseCodeAndMessage(resp any) (int, string, bool) {
	if resp == nil {
		return 0, "", false
	}
	value := reflect.ValueOf(resp)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, "", false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return 0, "", false
	}
	codeField := value.FieldByName("Code")
	if !codeField.IsValid() || !codeField.CanInt() {
		return 0, "", false
	}
	msg := ""
	msgField := value.FieldByName("Msg")
	if msgField.IsValid() && msgField.Kind() == reflect.String {
		msg = msgField.String()
	}
	return int(codeField.Int()), msg, true
}
