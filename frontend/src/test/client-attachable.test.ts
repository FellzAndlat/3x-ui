import { describe, expect, it } from 'vitest';

import { isClientAttachableProtocol } from '@/lib/inbounds/client-attachable';

describe('client inbound picker capabilities', () => {
  it('includes the per-client protocols supported by the server', () => {
    for (const protocol of [
      'vmess',
      'vless',
      'trojan',
      'shadowsocks',
      'hysteria',
      'wireguard',
      'mtproto',
      'amneziawg',
      'tuic',
      'trusttunnel',
      'naive',
      'anytls',
      'shadowtls',
      'mieru',
      'vk-turn-proxy',
      'sudoku',
    ]) {
      expect(isClientAttachableProtocol(protocol), protocol).toBe(true);
    }
  });

  it('excludes listeners without individual client accounts', () => {
    for (const protocol of ['pingtunnel', 'http', 'mixed', 'tun', 'tunnel']) {
      expect(isClientAttachableProtocol(protocol), protocol).toBe(false);
    }
  });
});
