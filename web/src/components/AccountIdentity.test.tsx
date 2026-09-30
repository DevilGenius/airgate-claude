import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { AccountIdentity } from './AccountIdentity';

describe('Claude authentication icons', () => {
  it('retains session mode and expiry in the native tooltip', () => {
    render(<AccountIdentity accountType="session_key" context={{ credentials: { session_key: 'test', expires_at: '2030-01-01' } }} />);
    const icon = screen.getByRole('img', { name: 'Session Key · 会话 · 过期时间：2030-01-01' });
    expect(icon).toHaveAttribute('title', 'Session Key · 会话 · 过期时间：2030-01-01');
    expect(screen.queryByText('Session Key')).not.toBeInTheDocument();
  });

  it('distinguishes OAuth tokens from API keys', () => {
    const { rerender } = render(<AccountIdentity accountType="oauth" context={{ credentials: { access_token: 'test' } }} />);
    expect(screen.getByRole('img', { name: 'OAuth · 令牌' })).toBeInTheDocument();
    rerender(<AccountIdentity accountType="apikey" context={{ credentials: {} }} />);
    expect(screen.getByRole('img', { name: 'API Key' })).toBeInTheDocument();
  });
});
