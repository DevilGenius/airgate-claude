import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AccountForm } from './AccountForm';
import type { AccountFormProps } from '@devilgenius/airgate-theme/plugin';

function Harness({
  accountType,
  credentials = {},
  oauth,
  onBatchImport,
  onBatchModeChange = vi.fn(),
  onChange = vi.fn(),
  onSuggestedName = vi.fn(),
  onTypeChange = vi.fn(),
}: {
  accountType?: string;
  credentials?: Record<string, string>;
  oauth?: AccountFormProps['oauth'];
  onBatchImport?: AccountFormProps['onBatchImport'];
  onBatchModeChange?: NonNullable<AccountFormProps['onBatchModeChange']>;
  onChange?: NonNullable<AccountFormProps['onChange']>;
  onSuggestedName?: NonNullable<AccountFormProps['onSuggestedName']>;
  onTypeChange?: NonNullable<AccountFormProps['onAccountTypeChange']>;
}) {
  const [currentCredentials, setCurrentCredentials] = useState(credentials);
  const [currentType, setCurrentType] = useState(accountType);

  return (
    <AccountForm
      accountType={currentType}
      credentials={currentCredentials}
      mode="create"
      oauth={oauth}
      onBatchImport={onBatchImport}
      onBatchModeChange={onBatchModeChange}
      onSuggestedName={onSuggestedName}
      onAccountTypeChange={(next) => {
        setCurrentType(next);
        onTypeChange(next);
      }}
      onChange={(next) => {
        setCurrentCredentials(next);
        onChange(next);
      }}
    />
  );
}

function oauthBridge(overrides: Partial<NonNullable<AccountFormProps['oauth']>> = {}) {
  return {
    batchExchange: vi.fn(),
    exchange: vi.fn(),
    start: vi.fn(),
    ...overrides,
  };
}

describe('Claude AccountForm', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('switches to Claude Console API key credentials', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const onTypeChange = vi.fn();

    render(<Harness onChange={onChange} onTypeChange={onTypeChange} />);

    await user.click(screen.getByText('Claude Console'));
    await user.type(screen.getByPlaceholderText('sk-ant-api03-...'), 'sk-ant-api03-test');
    await user.type(screen.getByPlaceholderText('https://api.anthropic.com'), 'https://anthropic.proxy');

    expect(onTypeChange).toHaveBeenCalledWith('apikey');
    expect(onChange).toHaveBeenLastCalledWith({
      api_key: 'sk-ant-api03-test',
      base_url: 'https://anthropic.proxy',
    });
  });

  it('exchanges a single Session Key through the OAuth bridge', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const onSuggestedName = vi.fn();
    const onTypeChange = vi.fn();
    const oauth = oauthBridge({
      exchange: vi.fn().mockResolvedValue({
        accountName: 'Claude User',
        accountType: 'oauth',
        credentials: { access_token: 'access', refresh_token: 'refresh' },
      }),
    });

    render(
      <Harness
        oauth={oauth}
        onChange={onChange}
        onSuggestedName={onSuggestedName}
        onTypeChange={onTypeChange}
      />,
    );

    await user.type(screen.getByPlaceholderText('sk-ant-sid01-...'), 'sid-1');
    await user.click(screen.getByRole('button', { name: '获取 OAuth Token' }));

    await waitFor(() => expect(oauth.exchange).toHaveBeenCalledWith(JSON.stringify({ session_key: 'sid-1' })));
    expect(onSuggestedName).toHaveBeenCalledWith('Claude User');
    expect(onTypeChange).toHaveBeenCalledWith('oauth');
    expect(onChange).toHaveBeenLastCalledWith({
      access_token: 'access',
      refresh_token: 'refresh',
      session_key: 'sid-1',
    });
  });

  it('batch exchanges Session Keys and imports successful accounts', async () => {
    const user = userEvent.setup();
    const onBatchImport = vi.fn().mockResolvedValue({ failed: 0, imported: 1 });
    const onBatchModeChange = vi.fn();
    const oauth = oauthBridge({
      batchExchange: vi.fn().mockResolvedValue([
        {
          accountName: 'Batch Claude',
          accountType: 'oauth',
          credentials: { access_token: 'batch-access' },
          status: 'ok',
        },
        { error: 'bad session', status: 'failed' },
      ]),
    });

    render(<Harness oauth={oauth} onBatchImport={onBatchImport} onBatchModeChange={onBatchModeChange} />);

    await user.click(screen.getByText('批量'));
    await user.type(screen.getByPlaceholderText(/sk-ant-sid01/), 'sid-1\n# ignored\nsid-2');
    await user.click(screen.getByRole('button', { name: '批量导入' }));

    await waitFor(() => expect(oauth.batchExchange).toHaveBeenCalledWith(['sid-1', 'sid-2']));
    expect(onBatchImport).toHaveBeenCalledWith([
      {
        credentials: { access_token: 'batch-access' },
        name: 'Batch Claude',
        type: 'oauth',
      },
    ]);
    expect(await screen.findByText('成功 1')).toBeTruthy();
    expect(await screen.findByText('失败 1')).toBeTruthy();
    expect(onBatchModeChange).toHaveBeenCalledWith(true);
  });

  it('runs browser OAuth start, copy and callback exchange flow', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const onSuggestedName = vi.fn();
    const oauth = oauthBridge({
      exchange: vi.fn().mockResolvedValue({
        accountName: 'Browser Claude',
        accountType: 'oauth',
        credentials: { access_token: 'browser-access' },
      }),
      start: vi.fn().mockResolvedValue({ authorizeURL: 'https://claude-auth.example', state: 'state-2' }),
    });

    render(<Harness oauth={oauth} onChange={onChange} onSuggestedName={onSuggestedName} />);

    await user.click(screen.getByText('浏览器授权'));
    await user.click(screen.getByRole('button', { name: '生成授权链接' }));
    expect(await screen.findByDisplayValue('https://claude-auth.example')).toBeTruthy();

    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined);
    await user.click(screen.getByRole('button', { name: '复制授权链接' }));
    expect(writeText).toHaveBeenCalledWith('https://claude-auth.example');

    await user.type(screen.getByPlaceholderText('粘贴完整回调 URL'), 'http://localhost/callback?code=ok');
    await user.click(screen.getByRole('button', { name: '完成授权交换' }));

    await waitFor(() => expect(oauth.exchange).toHaveBeenCalledWith('http://localhost/callback?code=ok'));
    expect(onSuggestedName).toHaveBeenCalledWith('Browser Claude');
    expect(onChange).toHaveBeenLastCalledWith({
      access_token: 'browser-access',
    });
  });
});
