// Inbounds with per-client credentials. Keep all client pickers on the same
// capability list; listener-only protocols (for example Pingtunnel) stay out.
const CLIENT_ATTACHABLE_PROTOCOLS = new Set([
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
  'fptn',
  'openflux',
  'naive',
  'snell',
  'anytls',
  'shadowtls',
  'mieru',
  'vk-turn-proxy',
  'sudoku',
]);

export function isClientAttachableProtocol(protocol: string | undefined): boolean {
  return CLIENT_ATTACHABLE_PROTOCOLS.has((protocol || '').toLowerCase());
}
