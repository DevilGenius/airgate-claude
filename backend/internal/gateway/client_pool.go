package gateway

import (
	"sync"
	"time"

	"github.com/imroc/req/v3"
)

const (
	clientPoolMaxEntries      = 100000
	clientPoolIdleTTL         = 2 * time.Hour
	clientPoolCleanupInterval = 10 * time.Minute
)

type clientEntry struct {
	client     *req.Client
	lastUsedAt time.Time
}

// clientPool 缓存 claude.ai 请求使用的 req.Client，按 proxyURL 隔离。
type clientPool struct {
	mu              sync.Mutex
	clients         map[string]*clientEntry
	lastCleanupTime time.Time
}

func (p *clientPool) get(proxyURL string) *req.Client {
	if p == nil {
		return newReqClient(proxyURL)
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.clients[proxyURL]; ok {
		entry.lastUsedAt = now
		p.cleanupIdleLocked(now)
		return entry.client
	}
	p.cleanupIdleLocked(now)
	if len(p.clients) >= clientPoolMaxEntries {
		p.deleteOldestLocked()
	}
	client := newReqClient(proxyURL)
	p.clients[proxyURL] = &clientEntry{client: client, lastUsedAt: now}
	return client
}

func (p *clientPool) cleanupIdleLocked(now time.Time) {
	if now.Sub(p.lastCleanupTime) < clientPoolCleanupInterval && len(p.clients) < clientPoolMaxEntries {
		return
	}
	for proxyURL, entry := range p.clients {
		if now.Sub(entry.lastUsedAt) > clientPoolIdleTTL {
			closeReqClient(entry.client)
			delete(p.clients, proxyURL)
		}
	}
	p.lastCleanupTime = now
}

func (p *clientPool) deleteOldestLocked() {
	var oldestKey string
	var oldestAt time.Time
	for proxyURL, entry := range p.clients {
		if oldestKey == "" || entry.lastUsedAt.Before(oldestAt) {
			oldestKey = proxyURL
			oldestAt = entry.lastUsedAt
		}
	}
	if oldestKey != "" {
		closeReqClient(p.clients[oldestKey].client)
		delete(p.clients, oldestKey)
	}
}

func (p *clientPool) close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, entry := range p.clients {
		closeReqClient(entry.client)
	}
	p.clients = make(map[string]*clientEntry)
	p.lastCleanupTime = time.Now()
}

func closeReqClient(client *req.Client) {
	if client == nil || client.GetTransport() == nil {
		return
	}
	client.GetTransport().CloseIdleConnections()
}

// newReqClient 构建用于 claude.ai 请求的 req 客户端（Chrome TLS 指纹 + 绕过 Cloudflare）。
func newReqClient(proxyURL string) *req.Client {
	client := req.C().
		SetTimeout(60 * time.Second).
		ImpersonateChrome().
		SetCookieJar(nil)
	if proxyURL != "" {
		client.SetProxyURL(proxyURL)
	}
	return client
}
