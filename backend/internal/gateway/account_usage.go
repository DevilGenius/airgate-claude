package gateway

import "time"

// quotaInfo 是 Claude 插件私有的账号额度探测结果。
// 不进入 SDK 通用契约，避免把平台差异固化到 Core。
type quotaInfo struct {
	Extra map[string]string `json:"extra,omitempty"`
}

// accountUsageWindow 是 Claude 插件私有的账号用量窗口。
type accountUsageWindow struct {
	Key               string  `json:"key"`
	Label             string  `json:"label"`
	UsedPercent       float64 `json:"used_percent"`
	ResetAt           string  `json:"reset_at,omitempty"`
	ResetAfterSeconds int     `json:"reset_after_seconds,omitempty"`
	UpdatedAt         string  `json:"updated_at,omitempty"`
}

type accountUsageInfo struct {
	UpdatedAt string               `json:"updated_at"`
	Windows   []accountUsageWindow `json:"windows,omitempty"`
}

type accountUsageError struct {
	ID      int64  `json:"id"`
	Message string `json:"message"`
}

type accountUsageAccountsResponse struct {
	Accounts map[string]accountUsageInfo `json:"accounts"`
	Errors   []accountUsageError         `json:"errors,omitempty"`
}

func newAccountUsageWindow(key, label string, usedPercent float64, resetAt *time.Time, now time.Time) accountUsageWindow {
	window := accountUsageWindow{
		Key:         key,
		Label:       label,
		UsedPercent: usedPercent,
		UpdatedAt:   now.UTC().Format(time.RFC3339),
	}
	if resetAt != nil {
		window.ResetAt = resetAt.UTC().Format(time.RFC3339)
		if resetAt.After(now) {
			window.ResetAfterSeconds = int(resetAt.Sub(now).Seconds())
		}
	}
	return window
}
