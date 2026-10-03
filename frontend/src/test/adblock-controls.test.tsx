import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import AdBlockTab from '@/pages/adblock/AdBlockTab';
import { HttpUtil } from '@/utils';

function status(overrides = {}) {
  return {
    server: {
      enabled: false,
      outbound: '',
      limits: { maxConnections: 128, workers: 4, bodyMiB: 8, queueMs: 250 },
    },
    policies: [],
    enabled: true,
    sources: '',
    customDomains: 'ads.example',
    allowlist: '',
    autoUpdate: true,
    updateIntervalHours: 24,
    lastUpdate: '',
    domainCount: 1,
    sourceCount: 0,
    profile: 'custom',
    profiles: [],
    youtubeMode: 'compatible',
    youtubeVideoAdsSupported: false,
    scope: { inboundMode: 'all', inbounds: [], clientMode: 'all', clients: [] },
    scopeOptions: { inbounds: [], clients: [] },
    pausedUntil: '',
    sourceStatuses: [],
    application: { pending: false, lastError: '', appliedAt: '', nextRetry: '' },
    automation: { lastAttempt: '', lastError: '', retryCount: 0, nextRetry: '' },
    ...overrides,
  };
}
afterEach(() => vi.restoreAllMocks());
describe('AdBlock controls', () => {
  it('keeps controls disabled after a failed initial load and allows retry', async () => {
    const get = vi
      .spyOn(HttpUtil, 'get')
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValue({ success: true, msg: '', obj: status() });
    render(<AdBlockTab />);
    await screen.findByText('Не удалось загрузить настройки AdBlock');
    expect(
      (screen.getByRole('button', { name: 'Пауза на 15 минут' }) as HTMLButtonElement).disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'Повторить' }));
    await screen.findByText('AdBlock работает');
    expect(
      (screen.getByRole('button', { name: 'Пауза на 15 минут' }) as HTMLButtonElement).disabled,
    ).toBe(false);
    expect(get).toHaveBeenCalledTimes(2);
  });

  it('polls status without losing unsaved rules or marking metadata dirty', async () => {
    const intervals = vi.spyOn(window, 'setInterval');
    const get = vi
      .spyOn(HttpUtil, 'get')
      .mockResolvedValue({ success: true, msg: '', obj: status() });
    render(<AdBlockTab />);
    await screen.findByText('AdBlock работает');
    const save = screen.getByRole('button', { name: /Сохранить/ }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    get.mockResolvedValue({ success: true, msg: '', obj: status({ domainCount: 2 }) });
    await act(async () => {
      const callback = intervals.mock.calls.at(-1)?.[0];
      if (typeof callback !== 'function') throw new Error('polling not scheduled');
      callback();
    });
    expect(save.disabled).toBe(true);
    fireEvent.click(screen.getByText('Расширенные настройки'));
    const custom = screen.getByPlaceholderText(/ads.example.com\s+tracker.example.net/);
    fireEvent.change(custom, { target: { value: 'new.ads.example' } });
    await waitFor(() => expect(save.disabled).toBe(false));
    get.mockResolvedValue({ success: true, msg: '', obj: status({ domainCount: 3 }) });
    await act(async () => {
      const callback = intervals.mock.calls.at(-1)?.[0];
      if (typeof callback !== 'function') throw new Error('polling not scheduled');
      callback();
    });
    expect((custom as HTMLTextAreaElement).value).toBe('new.ads.example');
    expect(save.disabled).toBe(false);
  });
  it('retries saved-but-unapplied settings separately', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue({
      success: true,
      msg: '',
      obj: status({
        application: { pending: true, lastError: 'core failed', appliedAt: '', nextRetry: '' },
      }),
    });
    const post = vi
      .spyOn(HttpUtil, 'post')
      .mockResolvedValue({ success: true, msg: '', obj: status() });
    render(<AdBlockTab />);
    await screen.findByText('Изменения ожидают применения');
    fireEvent.click(screen.getByRole('button', { name: 'Применить' }));
    await screen.findByText('AdBlock работает');
    expect(post.mock.calls[0]?.[0]).toBe('/panel/api/adblock/apply');
  });
  it('pauses without changing the enabled preference', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue({ success: true, msg: '', obj: status() });
    const deadline = new Date(Date.now() + 15 * 60_000).toISOString();
    const post = vi
      .spyOn(HttpUtil, 'post')
      .mockResolvedValue({ success: true, msg: '', obj: status({ pausedUntil: deadline }) });
    render(<AdBlockTab />);
    await screen.findByText('AdBlock работает');
    fireEvent.click(screen.getByRole('button', { name: 'Пауза на 15 минут' }));
    await screen.findByText('AdBlock временно приостановлен');
    expect(post.mock.calls[0]?.[0]).toBe('/panel/api/adblock/pause');
    expect(post.mock.calls[0][1]).toEqual({ minutes: 15 });
    expect(screen.getByRole('button', { name: 'Возобновить' })).toBeTruthy();
  });
});

it('saves policy priority changes', async () => {
  const scope = { inboundMode: 'all', inbounds: [], clientMode: 'all', clients: [] };
  const policies = [
    { id: 'first', name: 'First', enabled: true, profile: 'off', scope },
    { id: 'second', name: 'Second', enabled: true, profile: 'off', scope },
  ];
  vi.spyOn(HttpUtil, 'get').mockResolvedValue({
    success: true,
    msg: '',
    obj: status({ policies }),
  });
  const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue({
    success: true,
    msg: '',
    obj: status({ policies: [...policies].reverse() }),
  });
  render(<AdBlockTab />);
  await screen.findByText('AdBlock работает');
  fireEvent.click(screen.getByText('Расширенные настройки'));
  await screen.findByDisplayValue('First');
  fireEvent.click(screen.getAllByRole('button', { name: 'Ниже' })[0]);
  fireEvent.click(screen.getByRole('button', { name: /Сохранить/ }));
  await waitFor(() => expect(post).toHaveBeenCalled());
  expect(
    (post.mock.calls[0][1] as { policies: { id: string }[] }).policies.map((policy) => policy.id),
  ).toEqual(['second', 'first']);
}, 30_000);

it('saves managed server settings with the ordinary AdBlock profile', async () => {
  vi.spyOn(HttpUtil, 'get').mockResolvedValue({ success: true, msg: '', obj: status() });
  const post = vi
    .spyOn(HttpUtil, 'post')
    .mockResolvedValue({ success: true, msg: '', obj: status() });
  render(<AdBlockTab />);
  await screen.findByText('AdBlock работает');
  fireEvent.click(screen.getByText('Расширенные настройки'));
  fireEvent.click(screen.getByRole('switch', { name: 'Экспериментальная серверная фильтрация' }));
  fireEvent.click(screen.getByRole('button', { name: /Сохранить/ }));
  await waitFor(() => expect(post).toHaveBeenCalled());
  expect(
    (post.mock.calls[0][1] as { server: { enabled: boolean; limits: { queueMs: number } } }).server,
  ).toMatchObject({ enabled: true, limits: { queueMs: 250 } });
});
