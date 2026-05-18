import { useEffect, useState, type CSSProperties } from 'react';
import type { AccountSurfaceProps } from '@doudou-start/airgate-theme/plugin';

interface UsageWindowItem {
  key?: string;
  label: string;
  display_label?: string;
  slot?: string;
  group?: string;
  used_percent: number;
  reset_seconds?: number;
  reset_at?: string;
}

function isUsageWindowItem(item: unknown): item is UsageWindowItem {
  if (item === null || typeof item !== 'object') return false;
  const record = item as Record<string, unknown>;
  return typeof record.label === 'string' && typeof record.used_percent === 'number';
}

function getUsageWindows(context: AccountSurfaceProps['context']): UsageWindowItem[] {
  const windows = context?.windows;
  if (!Array.isArray(windows)) return [];
  return windows.filter(isUsageWindowItem);
}

function useResetTick(enabled: boolean) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!enabled) return undefined;
    const timer = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(timer);
  }, [enabled]);

  return now;
}

function resolveResetSeconds(w: UsageWindowItem, now: number) {
  if (w.reset_at) {
    const delta = Date.parse(w.reset_at) - now;
    if (Number.isFinite(delta)) return Math.max(0, Math.floor(delta / 1000));
  }
  if (typeof w.reset_seconds === 'number') return w.reset_seconds;
  return 0;
}

function formatReset(seconds: number) {
  if (!seconds || seconds <= 0) return '-';
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return h > 0 ? `${d}d${h}h` : `${d}d`;
  if (h > 0) return m > 0 ? `${h}h${m}m` : `${h}h`;
  return `${m}m`;
}

function usageColor(pct: number) {
  if (pct < 50) return 'var(--ag-success)';
  if (pct < 80) return 'var(--ag-warning)';
  return 'var(--ag-danger)';
}

function windowOrder(w: UsageWindowItem) {
  const slot = (w.slot || '').toLowerCase();
  const group = (w.group || '').toLowerCase();
  const key = (w.key || '').toLowerCase();
  const label = w.label.toLowerCase();
  if (slot === '5h' || key === '5h' || label.startsWith('5h')) return 0;
  if ((slot === '7d' && group === 'base') || key === '7d' || label === '7d') return 1;
  if (group.includes('sonnet') || key.includes('sonnet') || label.includes('sonnet')) return 2;
  return 3;
}

const rootStyle: CSSProperties = {
  display: 'flex',
  minWidth: 0,
  flexDirection: 'column',
  justifyContent: 'center',
  gap: '0.25rem',
  fontFamily: 'var(--ag-font-mono)',
};

const rowStyle: CSSProperties = {
  display: 'grid',
  height: '1.25rem',
  minWidth: 0,
  gridTemplateColumns: '4rem minmax(3rem, 1fr) 1.375rem 2.5rem',
  alignItems: 'center',
  gap: '0.125rem',
};

const badgeStyle: CSSProperties = {
  display: 'inline-flex',
  minWidth: 0,
  alignItems: 'center',
  justifyContent: 'flex-start',
  overflow: 'hidden',
  borderRadius: '0.25rem',
  border: '1px solid var(--ag-glass-border)',
  background: 'var(--ag-bg-surface)',
  padding: '0 0.25rem',
  color: 'var(--ag-text-secondary)',
  fontSize: '0.6875rem',
  fontWeight: 600,
  lineHeight: 1,
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
};

const barStyle: CSSProperties = {
  height: '0.375rem',
  minWidth: 0,
  overflow: 'hidden',
  borderRadius: '999px',
  background: 'var(--ag-glass-border)',
};

const valueStyle: CSSProperties = {
  width: '100%',
  minWidth: 0,
  overflow: 'hidden',
  textAlign: 'right',
  fontSize: '0.625rem',
  fontWeight: 600,
  lineHeight: 1,
  fontVariantNumeric: 'tabular-nums',
  whiteSpace: 'nowrap',
};

const resetStyle: CSSProperties = {
  ...valueStyle,
  display: 'inline-flex',
  height: '100%',
  alignItems: 'center',
  justifyContent: 'flex-end',
  color: 'var(--ag-text-secondary)',
};

export function UsageWindow({ context }: AccountSurfaceProps) {
  const windows = [...getUsageWindows(context)].sort((a, b) => windowOrder(a) - windowOrder(b));
  const resetNow = useResetTick(windows.length > 0);
  if (windows.length === 0) return null;

  return (
    <div style={rootStyle}>
      {windows.map((w, index) => {
        const percent = Math.round(w.used_percent);
        const barPercent = Math.max(0, Math.min(100, percent));
        const color = usageColor(w.used_percent);
        const resetText = formatReset(resolveResetSeconds(w, resetNow));
        const displayLabel = w.display_label?.trim() || w.slot?.trim() || w.label;
        return (
          <div key={w.key || `${w.label}:${index}`} style={rowStyle}>
            <span style={badgeStyle} title={w.label}>{displayLabel}</span>
            <div style={barStyle}>
              <div
                style={{
                  width: `${barPercent}%`,
                  height: '100%',
                  borderRadius: '999px',
                  background: color,
                }}
              />
            </div>
            <span style={{ ...valueStyle, color }}>{percent}%</span>
            <span style={resetStyle} title={resetText}>{resetText}</span>
          </div>
        );
      })}
    </div>
  );
}
