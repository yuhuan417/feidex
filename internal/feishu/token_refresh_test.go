package feishu

import (
	"context"
	"testing"
	"time"
)

// seedTokenCache 写入一条缓存项,模拟 SDK 侧落进共享缓存的 token。
func seedTokenCache(t *testing.T, cache *resettableLarkTokenCache, key, value string) {
	t.Helper()
	if err := cache.Set(context.Background(), key, value, time.Hour); err != nil {
		t.Fatalf("seed cache key %q: %v", key, err)
	}
}

func cacheKeys(cache *resettableLarkTokenCache) map[string]bool {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	out := make(map[string]bool, len(cache.values))
	for key := range cache.values {
		out[key] = true
	}
	return out
}

// 多应用隔离:清除一个应用不得影响其余应用。这是 P0 的核心回归点——
// 共享缓存被所有应用复用,按 appID 分段匹配是唯一的隔离手段。
func TestClearTenantAccessTokensIsolatesApps(t *testing.T) {
	cache := newResettableLarkTokenCache()
	const (
		appA = "cli_aaa"
		appB = "cli_bbb"
	)
	keyA := "tenant_access_token:app_secret:" + appA + ":fp-a:tenant-1"
	keyB := "tenant_access_token:app_secret:" + appB + ":fp-b:tenant-1"
	seedTokenCache(t, cache, keyA, "token-a")
	seedTokenCache(t, cache, keyB, "token-b")

	cleared := cache.clearTenantAccessTokens(appA)
	if cleared != 1 {
		t.Fatalf("cleared = %d, want 1", cleared)
	}

	keys := cacheKeys(cache)
	if keys[keyA] {
		t.Fatalf("app A token still present after clear")
	}
	if !keys[keyB] {
		t.Fatalf("app B token was removed by clearing app A (isolation broken)")
	}
}

// app ticket 的 key 是短横线分隔,不应被 token 清除判据命中——
// 清除范围必须与"只清 token"的语义一致。
func TestClearTenantAccessTokensLeavesAppTicket(t *testing.T) {
	cache := newResettableLarkTokenCache()
	const appID = "cli_aaa"
	tokenKey := "tenant_access_token:app_secret:" + appID + ":fp:tenant-1"
	ticketKey := "app_ticket-" + appID
	seedTokenCache(t, cache, tokenKey, "token")
	seedTokenCache(t, cache, ticketKey, "ticket")

	if cleared := cache.clearTenantAccessTokens(appID); cleared != 1 {
		t.Fatalf("cleared = %d, want 1", cleared)
	}

	keys := cacheKeys(cache)
	if keys[tokenKey] {
		t.Fatalf("token still present after clear")
	}
	if !keys[ticketKey] {
		t.Fatalf("app ticket was removed; clear criterion must only match token keys")
	}
}

// 判据不得按 "_" 切分:appID 形如 cli_xxx 本身含下划线,
// 切分会破坏 appID 导致匹配失败。
func TestTokenKeyBelongsToAppHandlesUnderscoreAppID(t *testing.T) {
	const appID = "cli_a1b2c3d4e5f6g7h8"
	key := "tenant_access_token:app_secret:" + appID + ":fingerprint:tenant-key"
	if !tokenKeyBelongsToApp(key, appID) {
		t.Fatalf("appID with underscores not matched in %q", key)
	}
}

// 负例:appID 仅是别的段的子串时不应误判。
func TestTokenKeyBelongsToAppRejectsSubstring(t *testing.T) {
	key := "tenant_access_token:app_secret:cli_aaa_extra:fp:tenant-1"
	if tokenKeyBelongsToApp(key, "cli_aaa") {
		t.Fatalf("appID matched as a substring of another segment")
	}
}

// 清除多个 token 类型:tenant_access_token 与 app_access_token 同属该应用,
// 但两者之间不能互相误伤。
func TestClearTenantAccessTokensCoversBothTokenTypes(t *testing.T) {
	cache := newResettableLarkTokenCache()
	const appID = "cli_aaa"
	tenantKey := "tenant_access_token:app_secret:" + appID + ":fp:tenant-1"
	appKey := "app_access_token:app_secret:" + appID + ":fp"
	seedTokenCache(t, cache, tenantKey, "tenant")
	seedTokenCache(t, cache, appKey, "app")

	if cleared := cache.clearTenantAccessTokens(appID); cleared != 2 {
		t.Fatalf("cleared = %d, want 2", cleared)
	}
	if keys := cacheKeys(cache); len(keys) != 0 {
		t.Fatalf("expected empty cache, got %v", keys)
	}
}
