package feishu

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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

// tokenKeyBelongsToApp 判断缓存 key 是否归属该应用。
//
// 依据:v3.12.0 起 SDK 的 token key 全部以 ":" 拼装,appID 是其中独立一段
// (core/tokenmanager.go:160-169)。多应用共用一份缓存时,appID 必须作为
// 独立段出现,否则 key 本就会冲突——这是 SDK 无法回避的约束。
func tokenKeyBelongsToApp(key, appID string) bool {
	for _, segment := range strings.Split(key, ":") {
		if segment == appID {
			return true
		}
	}
	return false
}

// clearTenantAccessTokens 清除指定应用的 token 缓存项,返回删除条数。
//
// 判据是"按 : 切分后取 appID 段",而不是匹配完整的 key 前缀。三点依据:
//
//  1. 为何按 ":" 切:v3.12.0 起 token key 形如
//     "tenant_access_token:app_secret:{appID}:{fingerprint}:{tenantKey}"。
//     旧版是 "-" 分隔,按完整前缀匹配会在升级后静默失效——这正是本次改动的起因。
//  2. 为何不能按 "_" 切:appID 形如 "cli_xxx" 本身含下划线,切分会破坏 appID。
//  3. 为何不能退回前缀匹配:前缀同时编码了 token 类型与分隔符两重假设,
//     SDK 任一处变更都会让自愈再次静默失效,而测试断言的是我们自己假设的格式、
//     不会失败。若将来分隔符再变,靠 refreshClientAfterInvalidTenantToken 中
//     cleared == 0 的金丝雀日志发现。
//
// 注意:app ticket 的 key 是 "{prefix}-{appID}"(短横线,core/appticketmanager.go:50),
// 不以 ":" 分隔,故不会命中——与本函数"只清 token"的语义一致。
//
// 另注:本判据会一并清掉 app_access_token(v3.5.3 的旧前缀匹配不到它)。
// 本项目使用 tenant token 模式,该 key 通常不存在。
func (c *resettableLarkTokenCache) clearTenantAccessTokens(appID string) int {
	if c == nil {
		return 0
	}
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for key := range c.values {
		if tokenKeyBelongsToApp(key, appID) {
			delete(c.values, key)
			count++
		}
	}
	return count
}

// newFeishuLarkClient 构造飞书客户端。
//
// 不变量:所有应用(每个 frontend 一个)必须共用同一个 sharedFeishuTokenCache 实例。
// larkcore.NewCache 会把传入的 cache 写进包级全局 tokenManager(client.go:275),
// 而请求时取用的正是该全局(core/reqtranslator.go:149)。多应用下若某个应用传入
// 独立 cache,全局会静默指向它,其余应用的 token 读写随之漂移。
func newFeishuLarkClient(cfg config.FeishuConfig, httpClient larkcore.HttpClient) *lark.Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return lark.NewClient(cfg.AppID, cfg.AppSecret,
		lark.WithTokenCache(sharedFeishuTokenCache),
		lark.WithOpenBaseUrl(cfg.OpenBaseURL()),
		lark.WithHttpClient(httpClient),
	)
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
	if cleared == 0 {
		// 金丝雀:能收到 99991663 说明确实使用过 token,正常情况下缓存中必有
		// 该应用的条目。恒为 0 意味着清除判据已匹配不到 SDK 的 key 格式
		// (例如分隔符再次变更,见 clearTenantAccessTokens 的说明)。
		slog.Warn("feishu token cache clear matched nothing; SDK cache key format may have changed",
			"api", strings.TrimSpace(api),
			"app_id", appID,
		)
	}

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
