package gateway

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ──────────────────────────────────────────────────────
// 传输连接池：复用 HTTP Transport，避免每次请求创建新连接
// ──────────────────────────────────────────────────────

const (
	standardTransportMaxEntries      = 100000
	standardTransportIdleTTL         = 2 * time.Hour
	standardTransportCleanupInterval = 10 * time.Minute
)

type standardTransportEntry struct {
	transport  *http.Transport
	lastUsedAt time.Time
}

// StandardTransportPool API Key 账号的标准 TLS 连接池
// 按 proxyURL 分组缓存 Transport
type StandardTransportPool struct {
	mu              sync.RWMutex
	pool            map[string]*standardTransportEntry // key = proxyURL (空字符串表示直连)
	dialer          *net.Dialer
	lastCleanupTime time.Time
}

// NewStandardTransportPool 创建标准 Transport 连接池
func NewStandardTransportPool() *StandardTransportPool {
	return &StandardTransportPool{
		pool:            make(map[string]*standardTransportEntry),
		lastCleanupTime: time.Now(),
		dialer: &net.Dialer{
			Timeout:   httpDialTimeout,
			KeepAlive: 30 * time.Second,
		},
	}
}

// Get 获取或创建 Transport
func (p *StandardTransportPool) Get(proxyURL string) *http.Transport {
	now := time.Now()
	p.mu.RLock()
	if entry, ok := p.pool[proxyURL]; ok {
		t := entry.transport
		p.mu.RUnlock()
		p.touch(proxyURL, now)
		return t
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check
	if entry, ok := p.pool[proxyURL]; ok {
		entry.lastUsedAt = now
		return entry.transport
	}

	p.cleanupIdleLocked(now)
	if len(p.pool) >= standardTransportMaxEntries {
		p.deleteOldestLocked()
	}
	t := &http.Transport{
		DialContext:           p.dialer.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   httpTLSTimeout,
		ResponseHeaderTimeout: httpResponseHeaderTimeout,
		MaxIdleConns:          httpMaxIdleConns,
		MaxIdleConnsPerHost:   httpIdleConnsPerHost,
		MaxConnsPerHost:       httpMaxConnsPerHost,
		IdleConnTimeout:       httpIdleTimeout,
		ForceAttemptHTTP2:     true,
	}

	if proxyURL != "" {
		if u, err := url.Parse(proxyURL); err == nil {
			t.Proxy = http.ProxyURL(u)
		}
	}

	p.pool[proxyURL] = &standardTransportEntry{transport: t, lastUsedAt: now}
	return t
}

func (p *StandardTransportPool) touch(proxyURL string, now time.Time) {
	p.mu.Lock()
	if entry, ok := p.pool[proxyURL]; ok {
		entry.lastUsedAt = now
	}
	p.cleanupIdleLocked(now)
	p.mu.Unlock()
}

func (p *StandardTransportPool) cleanupIdleLocked(now time.Time) {
	if now.Sub(p.lastCleanupTime) < standardTransportCleanupInterval && len(p.pool) < standardTransportMaxEntries {
		return
	}
	for proxyURL, entry := range p.pool {
		if now.Sub(entry.lastUsedAt) > standardTransportIdleTTL {
			entry.transport.CloseIdleConnections()
			delete(p.pool, proxyURL)
		}
	}
	p.lastCleanupTime = now
}

func (p *StandardTransportPool) deleteOldestLocked() {
	var oldestKey string
	var oldestAt time.Time
	for proxyURL, entry := range p.pool {
		if oldestKey == "" || entry.lastUsedAt.Before(oldestAt) {
			oldestKey = proxyURL
			oldestAt = entry.lastUsedAt
		}
	}
	if oldestKey != "" {
		p.pool[oldestKey].transport.CloseIdleConnections()
		delete(p.pool, oldestKey)
	}
}

// Close 关闭所有 Transport
func (p *StandardTransportPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, entry := range p.pool {
		entry.transport.CloseIdleConnections()
	}
	p.pool = make(map[string]*standardTransportEntry)
	p.lastCleanupTime = time.Now()
}

// FingerprintTransportPool OAuth/session_key 的 uTLS 指纹连接池
// 按 (accountID, proxyURL) 分桶：不同账号持有独立 Transport，
// 互不共享连接 / session ticket / PSK，使得多账号在上游看来是独立 CLI 实例。
const (
	fingerprintTransportMaxEntries      = 100000
	fingerprintTransportIdleTTL         = 2 * time.Hour
	fingerprintTransportCleanupInterval = 10 * time.Minute
)

type fingerprintTransportEntry struct {
	transport  *http.Transport
	lastUsedAt time.Time
}

type FingerprintTransportPool struct {
	mu              sync.RWMutex
	pool            map[string]*fingerprintTransportEntry // key = "accountID|proxyURL|profile"
	lastCleanupTime time.Time
}

// NewFingerprintTransportPool 创建 TLS 指纹 Transport 连接池
func NewFingerprintTransportPool() *FingerprintTransportPool {
	return &FingerprintTransportPool{
		pool:            make(map[string]*fingerprintTransportEntry),
		lastCleanupTime: time.Now(),
	}
}

// fpKey 生成池 key：tls_profile 变化时自动换 bucket
func fpKey(accountID int64, proxyURL, profile string) string {
	return fmt.Sprintf("%d|%s|%s", accountID, proxyURL, profile)
}

// Get 获取或创建指纹化 Transport（按账号 + profile 隔离）
func (p *FingerprintTransportPool) Get(accountID int64, proxyURL, profile string) *http.Transport {
	key := fpKey(accountID, proxyURL, profile)
	now := time.Now()

	p.mu.RLock()
	if entry, ok := p.pool[key]; ok {
		t := entry.transport
		p.mu.RUnlock()
		p.touch(key, now)
		return t
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.pool[key]; ok {
		entry.lastUsedAt = now
		return entry.transport
	}

	p.cleanupIdleLocked(now)
	if len(p.pool) >= fingerprintTransportMaxEntries {
		p.deleteOldestLocked()
	}
	t := buildFingerprintTransportWithProfile(proxyURL, profile)
	p.pool[key] = &fingerprintTransportEntry{transport: t, lastUsedAt: now}
	return t
}

func (p *FingerprintTransportPool) touch(key string, now time.Time) {
	p.mu.Lock()
	if entry, ok := p.pool[key]; ok {
		entry.lastUsedAt = now
	}
	p.cleanupIdleLocked(now)
	p.mu.Unlock()
}

func (p *FingerprintTransportPool) cleanupIdleLocked(now time.Time) {
	if now.Sub(p.lastCleanupTime) < fingerprintTransportCleanupInterval && len(p.pool) < fingerprintTransportMaxEntries {
		return
	}
	for key, entry := range p.pool {
		if now.Sub(entry.lastUsedAt) > fingerprintTransportIdleTTL {
			entry.transport.CloseIdleConnections()
			delete(p.pool, key)
		}
	}
	p.lastCleanupTime = now
}

func (p *FingerprintTransportPool) deleteOldestLocked() {
	var oldestKey string
	var oldestAt time.Time
	for key, entry := range p.pool {
		if oldestKey == "" || entry.lastUsedAt.Before(oldestAt) {
			oldestKey = key
			oldestAt = entry.lastUsedAt
		}
	}
	if oldestKey != "" {
		p.pool[oldestKey].transport.CloseIdleConnections()
		delete(p.pool, oldestKey)
	}
}

// RemoveAccount 移除指定账号的指纹 Transport，供账号删除/禁用事件调用。
func (p *FingerprintTransportPool) RemoveAccount(accountID int64) {
	if p == nil {
		return
	}
	prefix := strconv.FormatInt(accountID, 10) + "|"
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, entry := range p.pool {
		if strings.HasPrefix(key, prefix) {
			entry.transport.CloseIdleConnections()
			delete(p.pool, key)
		}
	}
}

// Close 关闭所有 Transport
func (p *FingerprintTransportPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, entry := range p.pool {
		entry.transport.CloseIdleConnections()
	}
	p.pool = make(map[string]*fingerprintTransportEntry)
	p.lastCleanupTime = time.Now()
}

// getHTTPClient 根据账号类型从连接池获取 HTTP Client
func getHTTPClient(stdPool *StandardTransportPool, fpPool *FingerprintTransportPool, accountID int64, accountType, proxyURL, tlsProfile string) *http.Client {
	var transport http.RoundTripper
	switch accountType {
	case "oauth", "session_key":
		if fpPool != nil {
			transport = fpPool.Get(accountID, proxyURL, tlsProfile)
		} else {
			transport = buildFingerprintTransportWithProfile(proxyURL, tlsProfile)
		}
	default:
		if stdPool != nil {
			transport = stdPool.Get(proxyURL)
		} else {
			transport = &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   httpDialTimeout,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
				TLSHandshakeTimeout:   httpTLSTimeout,
				ResponseHeaderTimeout: httpResponseHeaderTimeout,
				MaxIdleConns:          httpMaxIdleConns,
				MaxIdleConnsPerHost:   httpIdleConnsPerHost,
				MaxConnsPerHost:       httpMaxConnsPerHost,
				IdleConnTimeout:       httpIdleTimeout,
				ForceAttemptHTTP2:     true,
			}
		}
	}

	return &http.Client{
		Transport: transport,
	}
}

// poolStats 返回连接池统计（用于调试日志）
func poolStats(stdPool *StandardTransportPool, fpPool *FingerprintTransportPool) string {
	stdCount, fpCount := 0, 0
	if stdPool != nil {
		stdPool.mu.RLock()
		stdCount = len(stdPool.pool)
		stdPool.mu.RUnlock()
	}
	if fpPool != nil {
		fpPool.mu.RLock()
		fpCount = len(fpPool.pool)
		fpPool.mu.RUnlock()
	}
	return fmt.Sprintf("standard=%d, fingerprint=%d", stdCount, fpCount)
}
