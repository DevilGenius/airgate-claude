package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

// ──────────────────────────────────────────────────────
// Token 刷新管理器：进程内锁 + double-check + 错误分类重试
// 参考 sub2api 的 OAuthRefreshAPI.RefreshIfNeeded() 模式
// ──────────────────────────────────────────────────────

const (
	tokenRefreshSkew    = 3 * time.Minute  // 提前刷新窗口
	refreshCooldown     = 60 * time.Second // 已知失败的冷却窗口
	maxRefreshRetries   = 2                // 最大重试次数
	refreshRetryBackoff = 1 * time.Second  // 重试退避间隔

	tokenStateMaxEntries      = 100000
	tokenStateIdleTTL         = 24 * time.Hour
	tokenStateCleanupInterval = 30 * time.Minute
)

// tokenManager 管理 OAuth token 的并发安全刷新
type tokenManager struct {
	gateway         *AnthropicGateway
	logger          *slog.Logger
	mu              sync.Mutex
	locks           map[int64]*accountRefreshState
	lastCleanupTime time.Time
}

// accountRefreshState 单个账号的刷新状态
type accountRefreshState struct {
	mu               sync.Mutex
	lastRefreshAt    time.Time
	lastToken        string // 上次刷新后的 access_token（用于 double-check）
	lastRefreshToken string
	lastExpiresAt    time.Time
	lastExpiresAtRaw string
	lastError        error
	lastErrorAt      time.Time
	lastSeenAt       time.Time
}

type tokenRefreshError struct {
	err          error
	accountFault bool
}

func (e *tokenRefreshError) Error() string {
	return e.err.Error()
}

func (e *tokenRefreshError) Unwrap() error {
	return e.err
}

func newTokenRefreshError(err error, accountFault bool) error {
	if err == nil {
		return nil
	}
	return &tokenRefreshError{err: err, accountFault: accountFault}
}

func isAccountTokenRefreshError(err error) bool {
	var refreshErr *tokenRefreshError
	return errors.As(err, &refreshErr) && refreshErr.accountFault
}

// newTokenManager 创建 token 管理器
func newTokenManager(gw *AnthropicGateway, logger *slog.Logger) *tokenManager {
	return &tokenManager{
		gateway:         gw,
		logger:          logger,
		locks:           make(map[int64]*accountRefreshState),
		lastCleanupTime: time.Now(),
	}
}

// getState 获取或创建账号的刷新状态
func (m *tokenManager) getState(accountID int64) *accountRefreshState {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)
	if state, ok := m.locks[accountID]; ok {
		state.lastSeenAt = now
		return state
	}
	if len(m.locks) >= tokenStateMaxEntries {
		m.deleteOldestLocked()
	}
	state := &accountRefreshState{lastSeenAt: now}
	m.locks[accountID] = state
	return state
}

func (m *tokenManager) cleanupLocked(now time.Time) {
	if now.Sub(m.lastCleanupTime) < tokenStateCleanupInterval && len(m.locks) < tokenStateMaxEntries {
		return
	}
	for accountID, state := range m.locks {
		if now.Sub(state.lastSeenAt) > tokenStateIdleTTL {
			delete(m.locks, accountID)
		}
	}
	m.lastCleanupTime = now
}

func (m *tokenManager) deleteOldestLocked() {
	var oldestID int64
	var oldestAt time.Time
	found := false
	for accountID, state := range m.locks {
		if !found || state.lastSeenAt.Before(oldestAt) {
			oldestID = accountID
			oldestAt = state.lastSeenAt
			found = true
		}
	}
	if found {
		delete(m.locks, oldestID)
	}
}

func (s *accountRefreshState) freshCredentials(skew time.Duration) map[string]string {
	if s.lastToken == "" || s.lastExpiresAt.IsZero() || time.Until(s.lastExpiresAt) <= skew {
		return nil
	}
	updated := map[string]string{
		"access_token": s.lastToken,
		"expires_at":   s.lastExpiresAtRaw,
	}
	if s.lastRefreshToken != "" {
		updated["refresh_token"] = s.lastRefreshToken
	}
	return updated
}

func (s *accountRefreshState) rememberToken(accessToken, refreshToken string, expiresAt time.Time, expiresAtRaw string) {
	s.lastRefreshAt = time.Now()
	s.lastToken = accessToken
	s.lastRefreshToken = refreshToken
	s.lastExpiresAt = expiresAt
	s.lastExpiresAtRaw = expiresAtRaw
	s.lastError = nil
	s.lastErrorAt = time.Time{}
}

func applyCredentialUpdate(account *sdk.Account, updated map[string]string) bool {
	if account == nil || len(updated) == 0 {
		return false
	}
	changed := false
	if account.Credentials == nil {
		account.Credentials = make(map[string]string, len(updated))
	}
	for key, value := range updated {
		if value == "" {
			continue
		}
		if account.Credentials[key] != value {
			account.Credentials[key] = value
			changed = true
		}
	}
	return changed
}

// ensureValidToken 检查 token 过期状态，必要时自动刷新
// 返回更新后的凭证（用于回传 Core 持久化），如果没有刷新则为 nil
func (m *tokenManager) ensureValidToken(ctx context.Context, account *sdk.Account) (map[string]string, error) {
	// 对于 session_key 类型且没有 access_token 的情况，需要先做 exchange
	if account.Type == "session_key" && account.Credentials["access_token"] == "" {
		return m.ensureSessionKeyExchange(ctx, account)
	}

	refreshToken := account.Credentials["refresh_token"]
	if refreshToken == "" {
		return nil, nil // 没有 refresh_token，无法刷新
	}

	expiresAtStr := account.Credentials["expires_at"]
	if expiresAtStr == "" {
		return nil, nil // 没有过期时间信息，假设有效
	}

	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		m.logger.Warn("token_expires_at_parse_failed",
			sdk.LogFieldAccountID, account.ID,
			"expires_at", expiresAtStr,
			sdk.LogFieldError, err,
		)
		return nil, nil
	}

	// 未过期，无需刷新
	if time.Until(expiresAt) > tokenRefreshSkew {
		return nil, nil
	}

	return m.doRefresh(ctx, account)
}

// ensureSessionKeyExchange 使用 session_key 换取 OAuth token（加锁保护）
func (m *tokenManager) ensureSessionKeyExchange(ctx context.Context, account *sdk.Account) (map[string]string, error) {
	logger := sdk.LoggerFromContext(ctx)
	if logger == nil {
		logger = m.logger
	}
	sessionKey := account.Credentials["session_key"]
	if sessionKey == "" {
		err := fmt.Errorf("session key 账号缺少 session_key")
		logger.Warn("session_key_exchange_failed",
			sdk.LogFieldAccountID, account.ID,
			sdk.LogFieldError, err,
		)
		return nil, newTokenRefreshError(err, true)
	}

	state := m.getState(account.ID)
	state.mu.Lock()
	defer state.mu.Unlock()

	// Double-check: 另一个 goroutine 可能已经完成了 exchange
	if updated := state.freshCredentials(tokenRefreshSkew); len(updated) > 0 {
		if applyCredentialUpdate(account, updated) {
			return updated, nil
		}
		return nil, nil
	}
	if account.Credentials["access_token"] != "" {
		return nil, nil
	}

	logger.Debug("session_key_exchange_start", sdk.LogFieldAccountID, account.ID)

	exchangeStart := time.Now()
	tokenResp, err := m.gateway.ExchangeSessionKeyForToken(ctx, sessionKey, account.ProxyURL)
	if err != nil {
		logger.Warn("session_key_exchange_failed",
			sdk.LogFieldAccountID, account.ID,
			sdk.LogFieldDurationMs, time.Since(exchangeStart).Milliseconds(),
			sdk.LogFieldError, err,
		)
		wrapped := fmt.Errorf("session key 换取 token 失败: %w", err)
		return nil, newTokenRefreshError(wrapped, isNonRetryableRefreshError(err))
	}

	expiresAtTime := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	expiresAt := expiresAtTime.Format(time.RFC3339)

	// 更新内存中的 credentials
	account.Credentials["access_token"] = tokenResp.AccessToken
	account.Credentials["refresh_token"] = tokenResp.RefreshToken
	account.Credentials["expires_at"] = expiresAt

	updated := map[string]string{
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
		"expires_at":    expiresAt,
	}

	state.rememberToken(tokenResp.AccessToken, tokenResp.RefreshToken, expiresAtTime, expiresAt)

	logger.Debug("session_key_exchange_completed",
		sdk.LogFieldAccountID, account.ID,
		sdk.LogFieldDurationMs, time.Since(exchangeStart).Milliseconds(),
		"expires_at", expiresAt,
	)
	return updated, nil
}

// doRefresh 执行实际的 token 刷新（加锁 + double-check + 重试）
func (m *tokenManager) doRefresh(ctx context.Context, account *sdk.Account) (map[string]string, error) {
	logger := sdk.LoggerFromContext(ctx)
	if logger == nil {
		logger = m.logger
	}
	state := m.getState(account.ID)
	state.mu.Lock()
	defer state.mu.Unlock()

	// Double-check: 另一个 goroutine 可能已经刷新了。这里必须复用完整
	// access/refresh/expires_at，否则等待者会拿旧 refresh_token 再刷一次。
	if updated := state.freshCredentials(tokenRefreshSkew); len(updated) > 0 {
		if applyCredentialUpdate(account, updated) {
			logger.Debug("token_refresh_reused",
				sdk.LogFieldAccountID, account.ID,
				"refreshed_at", state.lastRefreshAt.Format(time.RFC3339),
			)
			return updated, nil
		}
		return nil, nil
	}

	// 检查冷却窗口：最近的错误是不可重试的，不重复刷新
	if state.lastError != nil && time.Since(state.lastErrorAt) < refreshCooldown {
		if isNonRetryableRefreshError(state.lastError) {
			logger.Warn("token_refresh_cooldown",
				sdk.LogFieldAccountID, account.ID,
				sdk.LogFieldError, state.lastError,
				"cooldown_remaining_ms", (refreshCooldown - time.Since(state.lastErrorAt)).Milliseconds(),
			)
			return nil, newTokenRefreshError(state.lastError, true)
		}
	}

	logger.Debug("token_refresh_start", sdk.LogFieldAccountID, account.ID)
	refreshStart := time.Now()

	refreshToken := account.Credentials["refresh_token"]
	proxyURL := account.ProxyURL
	tlsProfile := account.Credentials["tls_profile"]

	// 带重试的刷新
	var lastErr error
	for attempt := 0; attempt <= maxRefreshRetries; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(refreshRetryBackoff * time.Duration(attempt))
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		tokenResp, err := m.gateway.RefreshTokenForAccount(ctx, account.ID, refreshToken, proxyURL, tlsProfile)
		if err != nil {
			lastErr = err

			// 不可重试错误：立即停止
			if isNonRetryableRefreshError(err) {
				state.lastError = err
				state.lastErrorAt = time.Now()
				logger.Warn("token_refresh_failed",
					sdk.LogFieldAccountID, account.ID,
					sdk.LogFieldDurationMs, time.Since(refreshStart).Milliseconds(),
					sdk.LogFieldError, err,
					"attempt", attempt+1,
					sdk.LogFieldReason, "non_retryable",
				)
				return nil, newTokenRefreshError(err, true)
			}

			logger.Warn("token_refresh_retry",
				sdk.LogFieldAccountID, account.ID,
				sdk.LogFieldError, err,
				"attempt", attempt+1,
				"max_retries", maxRefreshRetries,
			)
			continue
		}

		// 刷新成功
		expiresAtTime := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		newExpiresAt := expiresAtTime.Format(time.RFC3339)

		// 更新内存中的 credentials
		account.Credentials["access_token"] = tokenResp.AccessToken
		account.Credentials["expires_at"] = newExpiresAt
		refreshTokenForState := refreshToken
		if tokenResp.RefreshToken != "" {
			account.Credentials["refresh_token"] = tokenResp.RefreshToken
			refreshTokenForState = tokenResp.RefreshToken
		}

		// 更新状态
		state.rememberToken(tokenResp.AccessToken, refreshTokenForState, expiresAtTime, newExpiresAt)

		// 构建回传给 Core 的更新凭证
		updated := map[string]string{
			"access_token": tokenResp.AccessToken,
			"expires_at":   newExpiresAt,
		}
		if tokenResp.RefreshToken != "" {
			updated["refresh_token"] = tokenResp.RefreshToken
		}

		logger.Debug("token_refresh_completed",
			sdk.LogFieldAccountID, account.ID,
			sdk.LogFieldDurationMs, time.Since(refreshStart).Milliseconds(),
			"new_expires_at", newExpiresAt,
			"attempt", attempt+1,
		)
		return updated, nil
	}

	// 重试耗尽：token endpoint 抖动按 transient 返回，不继续用过期 token 污染账号状态。
	state.lastError = lastErr
	state.lastErrorAt = time.Now()
	logger.Error("token_refresh_exhausted",
		sdk.LogFieldAccountID, account.ID,
		sdk.LogFieldDurationMs, time.Since(refreshStart).Milliseconds(),
		sdk.LogFieldError, lastErr,
	)
	return nil, newTokenRefreshError(lastErr, false)
}

// isNonRetryableRefreshError 判断刷新错误是否不可重试
// 移植自 sub2api token_refresh_service.go
func isNonRetryableRefreshError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	nonRetryableKeywords := []string{
		"invalid_grant",
		"invalid_client",
		"unauthorized_client",
		"access_denied",
		"missing_project_id",
		"no refresh token available",
	}
	for _, keyword := range nonRetryableKeywords {
		if strings.Contains(msg, keyword) {
			return true
		}
	}
	return false
}
