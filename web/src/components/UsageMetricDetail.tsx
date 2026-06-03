import type { CSSProperties, ReactNode } from 'react';
import type { UsageRecordSurfaceProps } from '@devilgenius/airgate-theme/plugin';

interface UsageRecordLike {
  model?: string;
  input_tokens?: number;
  output_tokens?: number;
  cached_input_tokens?: number;
  cache_creation_tokens?: number;
  reasoning_output_tokens?: number;
  usage_metadata?: Record<string, string>;
}

const TOKEN_COLORS = {
  cacheCreation: 'var(--ag-usage-token-cache-creation)',
  cacheRead: 'var(--ag-usage-token-cache-read)',
  input: 'var(--ag-usage-token-input)',
  output: 'var(--ag-usage-token-output)',
  reasoning: 'var(--ag-usage-token-reasoning)',
  total: 'var(--ag-text)',
} as const;

const panelStyle: CSSProperties = {
  overflow: 'hidden',
  borderRadius: 'var(--radius)',
};

const headerStyle: CSSProperties = {
  borderBottom: '1px solid var(--ag-border)',
  background: 'var(--ag-default-bg)',
  padding: '0.375rem 0.625rem',
};

const titleStyle: CSSProperties = {
  color: 'var(--ag-text)',
  fontSize: '0.875rem',
  fontWeight: 600,
  lineHeight: 1,
};

const subtitleStyle: CSSProperties = {
  marginTop: '0.25rem',
  overflow: 'hidden',
  color: 'var(--ag-text-tertiary)',
  fontSize: '0.75rem',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
};

const bodyStyle: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: '0.125rem',
  padding: '0.5rem',
};

const rowStyle: CSSProperties = {
  display: 'grid',
  gridTemplateColumns: 'minmax(0,1fr) minmax(7rem,max-content)',
  alignItems: 'center',
  gap: '0.75rem',
  borderRadius: 'var(--radius)',
  background: 'var(--ag-surface)',
  padding: '0.25rem 0.5rem',
  fontSize: '0.75rem',
};

const labelStyle: CSSProperties = {
  minWidth: 0,
  overflow: 'hidden',
  color: 'var(--ag-text-tertiary)',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
};

const inlineValueStyle: CSSProperties = {
  display: 'inline-flex',
  minWidth: 0,
  maxWidth: '100%',
  alignItems: 'baseline',
  justifyContent: 'flex-end',
  gap: '0.25rem',
};

const inlineValueMetaStyle: CSSProperties = {
  color: TOKEN_COLORS.reasoning,
  minWidth: 0,
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
};

const cacheCreationDetailStyle: CSSProperties = {
  ...inlineValueMetaStyle,
  color: TOKEN_COLORS.cacheRead,
};

const inlineValueNumberStyle: CSSProperties = {
  flexShrink: 0,
};

const valueStyle: CSSProperties = {
  minWidth: 0,
  maxWidth: '12rem',
  justifySelf: 'end',
  overflow: 'hidden',
  color: 'var(--ag-text-secondary)',
  fontFamily: 'var(--ag-font-mono)',
  fontWeight: 500,
  textAlign: 'right',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
};

const strongValueStyle: CSSProperties = {
  fontWeight: 700,
};

function recordFromContext(context: UsageRecordSurfaceProps['context']): UsageRecordLike {
  const record = context?.record;
  return record && typeof record === 'object' ? record as UsageRecordLike : {};
}

function metadataFromContext(context: UsageRecordSurfaceProps['context'], record: UsageRecordLike): Record<string, string> {
  const direct = context?.usage_metadata;
  if (direct && typeof direct === 'object') return direct as Record<string, string>;
  return record.usage_metadata ?? {};
}

function metadataNumber(metadata: Record<string, string>, key: string) {
  const value = Number(metadata[key]);
  return Number.isFinite(value) ? value : 0;
}

function formatNumber(value: number) {
  return Number.isInteger(value)
    ? value.toLocaleString()
    : value.toLocaleString(undefined, { maximumFractionDigits: 4 });
}

function Row({ label, strong, tone, value }: { label: ReactNode; strong?: boolean; tone?: string; value: ReactNode }) {
  return (
    <div style={rowStyle}>
      <span style={labelStyle}>{label}</span>
      <span style={{ ...valueStyle, ...(strong ? strongValueStyle : {}), color: tone }}>{value}</span>
    </div>
  );
}

function outputTokenValue(reasoningTokens: number, outputTokens: number) {
  return (
    <span style={inlineValueStyle}>
      {reasoningTokens > 0 ? (
        <span style={inlineValueMetaStyle}>(推理 {formatNumber(reasoningTokens)})</span>
      ) : null}
      <span style={inlineValueNumberStyle}>{formatNumber(outputTokens)}</span>
    </span>
  );
}

function cacheCreationTokenValue(totalTokens: number, cacheCreation5mTokens: number, cacheCreation1hTokens: number) {
  const parts: Array<[string, number]> = [];
  if (cacheCreation5mTokens > 0) parts.push(['5m', cacheCreation5mTokens]);
  if (cacheCreation1hTokens > 0) parts.push(['1h', cacheCreation1hTokens]);

  if (parts.length === 0) return formatNumber(totalTokens);

  return (
    <span style={inlineValueStyle}>
      <span style={cacheCreationDetailStyle}>
        ({parts.map(([label, value]) => `${label} ${formatNumber(value)}`).join(',')})
      </span>
      <span style={inlineValueNumberStyle}>{formatNumber(totalTokens)}</span>
    </span>
  );
}

export function UsageMetricDetail({ context }: UsageRecordSurfaceProps) {
  const record = recordFromContext(context);
  const metadata = metadataFromContext(context, record);
  const inputTokens = record.input_tokens || 0;
  const outputTokens = record.output_tokens || 0;
  const cacheReadTokens = record.cached_input_tokens || 0;
  const cacheCreationTokens = record.cache_creation_tokens || 0;
  const cacheCreation5mTokens = metadataNumber(metadata, 'claude.cache_creation_5m_tokens');
  const cacheCreation1hTokens = metadataNumber(metadata, 'claude.cache_creation_1h_tokens');
  const reasoningTokens = record.reasoning_output_tokens || 0;
  const totalTokens = inputTokens + outputTokens + cacheReadTokens + cacheCreationTokens;

  return (
    <div style={panelStyle}>
      <div style={headerStyle}>
        <div style={titleStyle}>Claude 计量明细</div>
        {record.model ? <div style={subtitleStyle}>{record.model}</div> : null}
      </div>
      <div style={bodyStyle}>
        <Row label="输入 Token" value={formatNumber(inputTokens)} tone={TOKEN_COLORS.input} />
        <Row label="输出 Token" value={outputTokenValue(reasoningTokens, outputTokens)} tone={TOKEN_COLORS.output} />
        {cacheReadTokens > 0 ? <Row label="缓存读取 Token" value={formatNumber(cacheReadTokens)} tone={TOKEN_COLORS.cacheRead} /> : null}
        {cacheCreationTokens > 0 ? (
          <Row
            label="缓存创建 Token"
            value={cacheCreationTokenValue(cacheCreationTokens, cacheCreation5mTokens, cacheCreation1hTokens)}
            tone={TOKEN_COLORS.cacheCreation}
          />
        ) : null}
        <Row label="总 Token" value={formatNumber(totalTokens)} tone={TOKEN_COLORS.total} strong />
      </div>
    </div>
  );
}
