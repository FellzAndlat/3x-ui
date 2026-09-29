import { describe, expect, it } from 'vitest';

import {
  canEnableReality,
  canEnableSniffing,
  canEnableStream,
  canEnableTls,
  canEnableTlsFlow,
  canEnableVisionSeed,
  isSS2022,
  isSSMultiUser,
} from '@/lib/xray/protocol-capabilities';

describe('protocol capability predicates', () => {
  it('gates TLS by protocol and transport', () => {
    for (const network of ['tcp', 'ws', 'http', 'grpc', 'httpupgrade', 'xhttp']) {
      expect(canEnableTls({ protocol: 'vless', streamSettings: { network } })).toBe(true);
    }
    for (const network of ['', 'kcp', 'quic']) {
      expect(canEnableTls({ protocol: 'vless', streamSettings: { network } })).toBe(false);
    }
    for (const protocol of ['vmess', 'vless', 'trojan', 'shadowsocks']) {
      expect(canEnableTls({ protocol, streamSettings: { network: 'tcp' } })).toBe(true);
    }
    for (const protocol of ['http', 'socks', 'wireguard', 'mtproto']) {
      expect(canEnableTls({ protocol, streamSettings: { network: 'tcp' } })).toBe(false);
    }

    // Hysteria terminates TLS itself and does not depend on the Xray transport.
    expect(canEnableTls({ protocol: 'hysteria', streamSettings: { network: 'kcp' } })).toBe(true);
  });

  it('gates REALITY by protocol and transport', () => {
    for (const network of ['tcp', 'http', 'grpc', 'xhttp']) {
      expect(canEnableReality({ protocol: 'vless', streamSettings: { network } })).toBe(true);
    }
    for (const network of ['', 'ws', 'kcp', 'httpupgrade']) {
      expect(canEnableReality({ protocol: 'vless', streamSettings: { network } })).toBe(false);
    }

    expect(canEnableReality({ protocol: 'trojan', streamSettings: { network: 'tcp' } })).toBe(true);
    expect(canEnableReality({ protocol: 'vmess', streamSettings: { network: 'tcp' } })).toBe(false);
  });

  it('enables Vision flow only for supported VLESS combinations', () => {
    expect(
      canEnableTlsFlow({
        protocol: 'vless',
        streamSettings: { network: 'tcp', security: 'tls' },
      }),
    ).toBe(true);
    expect(
      canEnableTlsFlow({
        protocol: 'vless',
        streamSettings: { network: 'tcp', security: 'reality' },
      }),
    ).toBe(true);
    expect(
      canEnableTlsFlow({
        protocol: 'vless',
        streamSettings: { network: 'tcp', security: 'none' },
      }),
    ).toBe(false);
    expect(
      canEnableTlsFlow({
        protocol: 'vless',
        settings: { encryption: 'mlkem768x25519plus.native.0rtt.test' },
        streamSettings: { network: 'xhttp', security: 'none' },
      }),
    ).toBe(true);
    expect(
      canEnableTlsFlow({
        protocol: 'vless',
        settings: { decryption: 'mlkem768x25519plus.native.0rtt.test' },
        streamSettings: { network: 'xhttp', security: 'none' },
      }),
    ).toBe(true);
    expect(
      canEnableTlsFlow({
        protocol: 'vless',
        settings: { encryption: 'none', decryption: '' },
        streamSettings: { network: 'xhttp', security: 'none' },
      }),
    ).toBe(false);
    expect(
      canEnableTlsFlow({
        protocol: 'vless',
        streamSettings: { network: 'ws', security: 'tls' },
      }),
    ).toBe(false);
    expect(
      canEnableTlsFlow({
        protocol: 'vmess',
        streamSettings: { network: 'tcp', security: 'tls' },
      }),
    ).toBe(false);
  });

  it('limits stream settings to protocols that expose them', () => {
    for (const protocol of [
      'vmess',
      'vless',
      'trojan',
      'shadowsocks',
      'hysteria',
      'wireguard',
      'tunnel',
    ]) {
      expect(canEnableStream({ protocol })).toBe(true);
    }
    for (const protocol of ['http', 'socks', 'mtproto', 'amneziawg', 'tuic', 'naive']) {
      expect(canEnableStream({ protocol })).toBe(false);
    }
  });

  it('disables Xray sniffing for external/non-Xray protocols', () => {
    for (const protocol of [
      'mtproto',
      'amneziawg',
      'tuic',
      'pingtunnel',
      'trusttunnel',
      'vk-turn-proxy',
      'naive',
      'psiphon',
      'mieru',
      'sudoku',
      'anytls',
      'shadowtls',
    ]) {
      expect(canEnableSniffing({ protocol })).toBe(false);
    }
    for (const protocol of ['vmess', 'vless', 'trojan', 'shadowsocks', 'hysteria', 'wireguard']) {
      expect(canEnableSniffing({ protocol })).toBe(true);
    }
  });

  it('requires both Vision-capable transport and a Vision client for the seed', () => {
    expect(
      canEnableVisionSeed({
        protocol: 'vless',
        streamSettings: { network: 'tcp', security: 'tls' },
        settings: { clients: [{ flow: 'xtls-rprx-vision' }] },
      }),
    ).toBe(true);
    expect(
      canEnableVisionSeed({
        protocol: 'vless',
        streamSettings: { network: 'tcp', security: 'tls' },
        settings: { clients: [{ flow: '' }] },
      }),
    ).toBe(false);
    expect(
      canEnableVisionSeed({
        protocol: 'vless',
        settings: {
          encryption: 'mlkem768x25519plus.native.0rtt.test',
          clients: [{ flow: 'xtls-rprx-vision' }],
        },
        streamSettings: { network: 'xhttp', security: 'none' },
      }),
    ).toBe(true);
    expect(
      canEnableVisionSeed({
        protocol: 'vless',
        streamSettings: { network: 'tcp', security: 'none' },
        settings: { clients: [{ flow: 'xtls-rprx-vision' }] },
      }),
    ).toBe(false);
    expect(
      canEnableVisionSeed({
        protocol: 'vless',
        streamSettings: { network: 'tcp', security: 'tls' },
      }),
    ).toBe(false);
  });

  it('classifies Shadowsocks 2022 and multi-user methods', () => {
    const cases = [
      {
        values: {
          protocol: 'shadowsocks',
          settings: { method: '2022-blake3-chacha20-poly1305' },
        },
        is2022: true,
        isMultiUser: false,
      },
      {
        values: {
          protocol: 'shadowsocks',
          settings: { method: '2022-blake3-aes-128-gcm' },
        },
        is2022: true,
        isMultiUser: true,
      },
      {
        values: { protocol: 'shadowsocks', settings: { method: 'aes-128-gcm' } },
        is2022: false,
        isMultiUser: true,
      },
      {
        values: { protocol: 'shadowsocks', settings: {} },
        is2022: false,
        isMultiUser: true,
      },
      {
        // Preserve the legacy quirk used by callers that narrow on protocol first.
        values: { protocol: 'vless', settings: {} },
        is2022: false,
        isMultiUser: true,
      },
    ];

    for (const { values, is2022, isMultiUser } of cases) {
      expect(isSS2022(values)).toBe(is2022);
      expect(isSSMultiUser(values)).toBe(isMultiUser);
    }
  });
});
