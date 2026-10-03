import { describe, expect, it } from 'vitest';
import {
  buildNativeSingBoxOutbound,
  normalizeOutboundJsonForPanel,
} from '@/pages/xray/outbounds/singbox-native-outbound';
import { SINGBOX_NATIVE_OUTBOUND_PROTOCOLS } from '@/pages/xray/outbounds/outbound-form-constants';
import { isUdpOutbound } from '@/hooks/useXraySetting';
import { isUntestable } from '@/pages/xray/outbounds/outbounds-tab-helpers';

describe('native sing-box outbound templates', () => {
  it.each(SINGBOX_NATIVE_OUTBOUND_PROTOCOLS)('builds a native %s wrapper', (protocol) => {
    const outbound = buildNativeSingBoxOutbound(protocol, 'chosen-tag');
    expect(outbound.protocol).toBe(protocol);
    expect(outbound.tag).toBe('chosen-tag');
    expect(outbound.settings).toBeTypeOf('object');
    if (['singbox:vmess', 'singbox:vless', 'singbox:tuic'].includes(protocol))
      expect(outbound.settings).toHaveProperty('uuid');
  });
  it('preserves all options from official JSON in the native wrapper', () => {
    const raw = {
      type: 'VLESS',
      tag: 'proxy',
      server: '::1',
      server_port: 443,
      uuid: 'id',
      packet_encoding: 'xudp',
      multiplex: { enabled: true },
      tls: { enabled: true },
    };
    const { type: _, tag, ...settings } = raw;
    expect(normalizeOutboundJsonForPanel(raw)).toEqual({
      protocol: 'singbox:vless',
      tag,
      settings,
    });
  });
  it.each(['singbox:hysteria', 'singbox:hysteria2', 'singbox:tuic', 'selector', 'singbox:urltest'])(
    'uses a handshake probe for %s',
    (protocol) => {
      expect(isUdpOutbound({ protocol })).toBe(true);
    },
  );
  it.each(['singbox:direct', 'singbox:block', 'singbox:dns'])(
    'disables testing for %s',
    (protocol) => {
      expect(isUntestable({ key: 0, tag: 'native', protocol })).toBe(true);
    },
  );
});

describe('outbound chain probe mode', () => {
  it('uses a handshake for a native detour', () => {
    expect(
      isUdpOutbound({ protocol: 'singbox:shadowsocks', settings: { detour: 'shadowtls' } }),
    ).toBe(true);
  });
  it('uses a handshake for a socket detour', () => {
    expect(
      isUdpOutbound({ protocol: 'vless', streamSettings: { sockopt: { dialerProxy: 'hop' } } }),
    ).toBe(true);
  });
  it('uses a handshake for native QUIC transport', () => {
    expect(
      isUdpOutbound({ protocol: 'singbox:vmess', settings: { transport: { type: 'quic' } } }),
    ).toBe(true);
  });
  it('keeps unchained TCP proxies in TCP mode', () => {
    expect(isUdpOutbound({ protocol: 'singbox:ssh', settings: { detour: '' } })).toBe(false);
  });
});
