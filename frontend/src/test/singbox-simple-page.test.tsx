import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import SingBoxPage from '@/pages/singbox/SingBoxPage';
import { HttpUtil } from '@/utils';

vi.mock('@/layouts/AppSidebar', () => ({ default: () => null }));
vi.mock('@/pages/adblock/AdBlockTab', () => ({ default: () => null }));
vi.mock('@/hooks/useTheme', () => ({
  useTheme: () => ({ antdThemeConfig: {}, isDark: false, isUltra: false }),
}));

const config = {
  log: { level: 'info', output: '/tmp/sing-box.log' },
  dns: {
    servers: [{ type: 'local', tag: 'local' }],
    final: 'local',
    rules: [{ domain_suffix: ['lan'], server: 'local' }],
  },
  route: { final: 'direct', rules: [{ action: 'reject', domain: ['ads.test'] }] },
  outbounds: [{ type: 'direct', tag: 'direct' }],
  inbounds: [{ type: 'vless', tag: 'managed' }],
  services: [{ type: 'resolved' }],
};

beforeEach(() => {
  vi.mocked(HttpUtil.get).mockResolvedValue({
    success: true,
    msg: '',
    obj: {
      config: structuredClone(config),
      running: true,
      version: '1.14.0',
      configSource: 'disk',
      managedSections: ['inbounds'],
    },
  });
  vi.mocked(HttpUtil.post).mockClear();
});

function mount(path = '/singbox') {
  render(
    <MemoryRouter initialEntries={[path]}>
      <SingBoxPage />
    </MemoryRouter>,
  );
}

describe('simple sing-box page', () => {
  it('starts with simple controls and only exposes expert sections after switching', async () => {
    mount();
    await screen.findByText('Быстрая настройка');
    expect(screen.queryByText('Endpoints')).toBeNull();
    expect(screen.queryByText('V2Ray API')).toBeNull();
    expect(screen.getByText('Save').closest('button')!.hasAttribute('disabled')).toBe(true);
    fireEvent.click(screen.getByLabelText('Расширенный режим'));
    expect(await screen.findByText('Endpoints')).toBeTruthy();
  });

  it('saves simple edits with custom rules intact and omits panel-managed sections', async () => {
    mount();
    fireEvent.click(await screen.findByLabelText('Кэш DNS'));
    fireEvent.click(screen.getByText('Save').closest('button')!);
    await waitFor(() =>
      expect(HttpUtil.post).toHaveBeenCalledWith(
        '/panel/api/setting/singbox/config',
        expect.anything(),
      ),
    );
    const call = vi
      .mocked(HttpUtil.post)
      .mock.calls.find(([path]) => path === '/panel/api/setting/singbox/config');
    expect(call).toBeDefined();
    const sent = JSON.parse((call![1] as { config: string }).config);
    const { inbounds: _inbounds, services: _services, ...unmanaged } = config;
    expect(sent).toEqual({ ...unmanaged, dns: { ...config.dns, disable_cache: true } });
  });

  it('asks before discarding unsaved edits', async () => {
    mount();
    await screen.findByText('Быстрая настройка');
    fireEvent.click(screen.getByLabelText('Кэш DNS'));
    const loads = vi.mocked(HttpUtil.get).mock.calls.length;
    fireEvent.click(screen.getByText('Refresh').closest('button')!);
    await screen.findAllByText('Отменить несохранённые изменения?');
    fireEvent.click(screen.getByText('Продолжить настройку').closest('button')!);
    expect(HttpUtil.get).toHaveBeenCalledTimes(loads);
    expect(HttpUtil.post).not.toHaveBeenCalled();
  });

  it('asks before resetting automatic settings', async () => {
    mount();
    await screen.findByText('Быстрая настройка');
    fireEvent.click(screen.getByLabelText('Расширенный режим'));
    fireEvent.click(screen.getByText('Reset to generated settings').closest('button')!);
    await screen.findAllByText('Вернуть автоматические настройки sing-box?');
    expect(HttpUtil.post).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText('Отмена').closest('button')!);
  });
  it('imports standard Trojan links with TLS enabled', async () => {
    mount('/singbox#outbound');
    fireEvent.click(await screen.findByText('Импорт ссылки'));
    fireEvent.change(screen.getByPlaceholderText('vless://...'), {
      target: { value: 'trojan://pass@proxy.test#Imported' },
    });
    fireEvent.click(screen.getByText('Добавить').closest('button')!);
    await screen.findByText('Imported');
    fireEvent.click(screen.getByText('Save').closest('button')!);
    await waitFor(() =>
      expect(HttpUtil.post).toHaveBeenCalledWith(
        '/panel/api/setting/singbox/config',
        expect.anything(),
      ),
    );
    const call = vi
      .mocked(HttpUtil.post)
      .mock.calls.find(([path]) => path === '/panel/api/setting/singbox/config')!;
    const sent = JSON.parse((call[1] as { config: string }).config);
    expect(sent.outbounds).toContainEqual({
      type: 'trojan',
      tag: 'Imported',
      server: 'proxy.test',
      server_port: 443,
      password: 'pass',
      tls: { enabled: true, server_name: 'proxy.test' },
    });
  });

  it('keeps unsupported links out of the configuration', async () => {
    mount('/singbox#outbound');
    fireEvent.click(await screen.findByText('Импорт ссылки'));
    fireEvent.change(screen.getByPlaceholderText('vless://...'), {
      target: {
        value: 'vless://12345678-1234-1234-1234-123456789abc@proxy.test?type=xhttp#Unsupported',
      },
    });
    fireEvent.click(screen.getByText('Добавить').closest('button')!);
    await screen.findByText(/Транспорт xhttp не поддерживается/);
    expect(screen.queryByText('Unsupported')).toBeNull();
    expect(screen.getByText('Save').closest('button')!.hasAttribute('disabled')).toBe(true);
    expect(HttpUtil.post).not.toHaveBeenCalled();
  });
});
