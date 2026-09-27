import { describe, expect, it } from 'vitest';

import { inboundNetworkLabels } from '@/pages/inbounds/list/helpers';

describe('inboundNetworkLabels', () => {
  it.each([
    ['anytls', {}, ['TCP']],
    ['shadowtls', {}, ['ShadowTLS', 'TCP']],
    ['mtproto', {}, ['TCP']],
    ['http', {}, ['TCP']],
    ['sudoku', {}, ['TCP']],
    ['naive', { network: 'udp' }, ['UDP']],
    ['naive', { network: 'tcp' }, ['TCP']],
    ['mieru', { tcpPorts: ['2012-2022'] }, ['TCP']],
    ['mieru', { udpPorts: ['2023-2033'] }, ['UDP']],
    ['mieru', { tcpPorts: ['2012'], udpPorts: ['2023'] }, ['TCP', 'UDP']],
    ['mieru', { protocols: ['UDP'] }, ['UDP']],
    ['vk-turn-proxy', { useUdp: true }, ['UDP']],
    ['vk-turn-proxy', { useUdp: false }, ['TCP']],
    ['hysteria', {}, ['UDP']],
    ['tuic', {}, ['UDP']],
    ['wireguard', {}, ['UDP']],
    ['amneziawg', {}, ['UDP']],
    ['mixed', { udp: true }, ['TCP,UDP']],
    ['tunnel', { allowedNetwork: 'tcp,udp' }, ['TCP,UDP']],
    ['tunnel', { allowedNetwork: 'udp' }, ['UDP']],
    ['tun', {}, []],
  ] as const)('%s %o -> %o', (protocol, settings, expected) => {
    expect(
      inboundNetworkLabels({
        protocol,
        settings,
        streamSettings: {},
      }),
    ).toEqual(expected);
  });

  it('uses stream transport labels for VLESS/VMess/Trojan', () => {
    expect(
      inboundNetworkLabels({
        protocol: 'vless',
        settings: {},
        streamSettings: JSON.stringify({ network: 'grpc', security: 'tls' }),
      }),
    ).toEqual(['GRPC']);
  });

  it('shows ShadowTLS over TCP for a wrapped inner protocol', () => {
    expect(
      inboundNetworkLabels({
        protocol: 'vless',
        settings: { shadowTls: { enabled: true } },
        streamSettings: {},
      }),
    ).toEqual(['ShadowTLS', 'TCP']);
  });

  it('adds the L4 tag for QUIC/KCP transports', () => {
    expect(
      inboundNetworkLabels({
        protocol: 'vless',
        settings: {},
        streamSettings: { network: 'quic' },
      }),
    ).toEqual(['QUIC', 'UDP']);
  });
});
