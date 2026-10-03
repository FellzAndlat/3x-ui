import { describe, expect, it } from 'vitest';
import {
  applyDnsPreset,
  currentDnsPreset,
  DNS_PRESETS,
  outboundDefaults,
  setDefaultOutbound,
  setDnsCache,
  setOptimisticCache,
  setLogLevel,
} from '@/pages/singbox/simple-settings';

describe('simple sing-box settings', () => {
  it('preserves custom rules, bootstrap resolver and managed sections', () => {
    const config = {
      dns: {
        servers: [{ type: 'local', tag: 'bootstrap' }],
        final: 'bootstrap',
        rules: [{ domain_suffix: ['lan'], server: 'bootstrap' }],
        strategy: 'ipv4_only',
      },
      route: {
        rules: [{ outbound: 'proxy' }],
        final: 'proxy',
        default_domain_resolver: { server: 'pinned-resolver', strategy: 'ipv4_only' },
      },
      inbounds: [{ type: 'vless', tag: 'in' }],
      services: [{ type: 'resolved' }],
      outbounds: [{ type: 'direct', tag: 'proxy' }],
      experimental: { clash_api: { secret: 'keep' } },
    };
    const original = structuredClone(config);
    expect(applyDnsPreset(config, 'cloudflare')).toEqual({
      ...config,
      dns: {
        ...config.dns,
        final: 'panel-dns-cloudflare',
        servers: [
          ...config.dns.servers,
          { ...DNS_PRESETS.cloudflare, tag: 'panel-dns-cloudflare' },
        ],
      },
    });
    expect(config).toEqual(original);
  });
  it('avoids tag collisions and reuses presets after wire serialization reorders keys', () => {
    const next = applyDnsPreset(
      { dns: { servers: [{ tag: 'panel-dns-cloudflare', type: 'udp', server: '192.0.2.1' }] } },
      'cloudflare',
    );
    const wire = JSON.parse(
      JSON.stringify(next, (_key, value) => {
        if (value && typeof value === 'object' && !Array.isArray(value))
          return Object.fromEntries(Object.entries(value).sort(([a], [b]) => a.localeCompare(b)));
        return value;
      }),
    );
    expect(wire.dns.final).toBe('panel-dns-cloudflare-2');
    expect(currentDnsPreset(wire)).toBe('cloudflare');
    expect(applyDnsPreset(wire, 'cloudflare')).toEqual(wire);
  });
  it('recognizes custom TLS or detours as custom settings', () => {
    expect(
      currentDnsPreset({
        dns: {
          final: 'custom',
          servers: [{ ...DNS_PRESETS.cloudflare, tag: 'custom', detour: 'proxy' }],
        },
      }),
    ).toBe('custom');
  });
  it('creates a resolver when missing and retains servers when switching presets', () => {
    const first = applyDnsPreset({}, 'quad9');
    expect(first.route).toEqual({ default_domain_resolver: 'panel-dns-quad9' });
    const second = applyDnsPreset(first, 'system');
    expect(currentDnsPreset(second)).toBe('system');
    expect(second.dns).toMatchObject({
      servers: [
        { ...DNS_PRESETS.quad9, tag: 'panel-dns-quad9' },
        { type: 'local', tag: 'panel-dns-system' },
      ],
    });
    expect(applyDnsPreset(second, 'quad9')).toMatchObject({
      dns: { final: 'panel-dns-quad9', servers: (second.dns as { servers: unknown[] }).servers },
    });
  });
  it('keeps domain resolution in sync with the chosen DNS while preserving resolver options', () => {
    const config = {
      dns: { final: 'local', servers: [{ type: 'local', tag: 'local' }] },
      route: { default_domain_resolver: { server: 'local', strategy: 'ipv4_only' } },
    };
    expect(applyDnsPreset(config, 'cloudflare').route).toEqual({
      default_domain_resolver: { server: 'panel-dns-cloudflare', strategy: 'ipv4_only' },
    });
    const next = applyDnsPreset(applyDnsPreset({}, 'cloudflare'), 'system');
    expect(next.route).toEqual({ default_domain_resolver: 'panel-dns-system' });
  });

  it('changes only the fallback outbound and removes final for automatic mode', () => {
    const config = {
      route: {
        rules: [{ action: 'reject', domain: ['ads.test'] }],
        default_domain_resolver: 'local',
        final: 'direct',
      },
      outbounds: [{ type: 'direct', tag: 'direct' }],
    };
    expect(setDefaultOutbound(config, 'proxy')).toEqual({
      ...config,
      route: { ...config.route, final: 'proxy' },
    });
    const { final: _final, ...route } = config.route;
    expect(setDefaultOutbound(config, '')).toEqual({ ...config, route });
    expect(config.route.final).toBe('direct');
  });
  it.each(['trojan', 'hysteria2', 'tuic'])(
    'enables verified TLS for new %s connections',
    (type) => {
      expect(outboundDefaults(type, 'proxy')).toMatchObject({
        type,
        tag: 'proxy',
        server_port: 443,
        tls: { enabled: true },
      });
      expect(outboundDefaults(type, 'proxy')).not.toHaveProperty('tls.insecure');
    },
  );
});

describe('dependent sing-box settings', () => {
  it('disables optimistic caching together with normal caching, retaining custom DNS rules', () => {
    const config = {
      dns: {
        optimistic: { enabled: true, timeout: '1h' },
        rules: [{ domain: ['internal.test'], server: 'local' }],
        servers: [{ type: 'local', tag: 'local' }],
      },
      experimental: { clash_api: { secret: 'keep' } },
    };
    expect(setDnsCache(config, false)).toEqual({
      ...config,
      dns: { ...config.dns, disable_cache: true, optimistic: false },
    });
    expect(config.dns.optimistic).toEqual({ enabled: true, timeout: '1h' });
  });
  it('enables optimistic caching without conflicting flags and keeps its timeout', () => {
    const config = {
      dns: {
        optimistic: { enabled: false, timeout: '2h' },
        disable_cache: true,
        disable_expire: true,
      },
    };
    expect(setOptimisticCache(config, true)).toEqual({
      dns: {
        optimistic: { enabled: true, timeout: '2h' },
        disable_cache: false,
        disable_expire: false,
      },
    });
    expect(setOptimisticCache({ dns: { optimistic: true } }, false)).toEqual({
      dns: { optimistic: false },
    });
  });
  it('changes log detail without losing custom output or disabling traffic accounting', () => {
    const config = {
      log: { output: '/tmp/custom.log', timestamp: true, level: 'trace' },
      experimental: { v2ray_api: { listen: '127.0.0.1:10085' } },
    };
    const disabled = setLogLevel(config, 'off');
    expect(disabled).toEqual({ ...config, log: { ...config.log, disabled: true } });
    expect(setLogLevel(disabled, 'warn')).toEqual({
      ...config,
      log: { ...config.log, level: 'warn', disabled: false },
    });
  });
});
