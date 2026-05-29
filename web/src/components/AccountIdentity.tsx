import type { CSSProperties } from 'react';
import type { AccountSurfaceProps } from '@devilgenius/airgate-theme/plugin';

type AccountLike = {
  type?: string;
  credentials?: Record<string, string>;
};

function readAccount(context: AccountSurfaceProps['context']): AccountLike {
  const account = context?.account;
  if (account && typeof account === 'object') return account as AccountLike;
  return {};
}

function typeLabel(type?: string) {
  if (type === 'oauth') return 'OAuth';
  if (type === 'apikey') return 'API Key';
  if (type === 'session_key') return 'Session Key';
  return type || '';
}

const rowStyle: CSSProperties = {
  display: 'flex',
  maxWidth: '100%',
  alignItems: 'center',
  justifyContent: 'center',
  gap: '0.25rem',
};

const badgeStyle: CSSProperties = {
  maxWidth: '100%',
  overflow: 'hidden',
  border: '1px solid var(--ag-glass-border)',
  borderRadius: '0.25rem',
  background: 'var(--ag-bg-surface)',
  padding: '0 0.25rem',
  color: 'var(--ag-text-secondary)',
  fontSize: '0.625rem',
  lineHeight: 1,
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
};

const modeBadgeStyle: CSSProperties = {
  ...badgeStyle,
  borderColor: 'color-mix(in srgb, var(--ag-primary) 28%, transparent)',
  background: 'color-mix(in srgb, var(--ag-primary) 12%, transparent)',
  color: 'var(--ag-primary)',
  fontWeight: 600,
};

export function AccountIdentity({ accountType, context }: AccountSurfaceProps) {
  const account = readAccount(context);
  const credentials = (context?.credentials as Record<string, string> | undefined) ?? account.credentials ?? {};
  const type = account.type || accountType;
  const mode = credentials.session_key
    ? '会话'
    : credentials.access_token
      ? '令牌'
      : '';
  const expiresAt = credentials.expires_at;

  return (
    <div style={rowStyle}>
      {type && <span style={badgeStyle}>{typeLabel(type)}</span>}
      {mode && <span style={modeBadgeStyle} title={expiresAt ? `过期时间：${expiresAt}` : undefined}>{mode}</span>}
    </div>
  );
}
