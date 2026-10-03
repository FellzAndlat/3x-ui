export type NativeSingBoxOutbound = {
  protocol: string;
  tag: string;
  settings: Record<string, unknown>;
};

const DEFAULT_PROBE_URL = 'https://www.gstatic.com/generate_204';

export function buildNativeSingBoxOutbound(protocol: string, tag = ''): NativeSingBoxOutbound {
  const normalized = protocol.trim().toLowerCase();
  const nativeType = normalized.replace(/^singbox:/, '');
  let settings: Record<string, unknown>;

  switch (nativeType) {
    case 'socks':
      settings = { server: '', server_port: 1080, version: '5' };
      break;
    case 'http':
      settings = { server: '', server_port: 8080 };
      break;
    case 'shadowsocks':
      settings = { server: '', server_port: 443, method: '2022-blake3-aes-128-gcm', password: '' };
      break;
    case 'vmess':
      settings = { server: '', server_port: 443, uuid: '', security: 'auto' };
      break;
    case 'vless':
      settings = { server: '', server_port: 443, uuid: '' };
      break;
    case 'trojan':
      settings = {
        server: '',
        server_port: 443,
        password: '',
        tls: { enabled: true, server_name: '' },
      };
      break;
    case 'hysteria':
      settings = {
        server: '',
        server_port: 443,
        up_mbps: 100,
        down_mbps: 100,
        tls: { enabled: true, server_name: '' },
      };
      break;
    case 'bridge':
      settings = { interface: '' };
      break;
    case 'shadowtls':
      settings = {
        server: '',
        server_port: 443,
        version: 3,
        password: '',
        tls: { enabled: true, server_name: '' },
      };
      break;
    case 'tuic':
      settings = {
        server: '',
        server_port: 443,
        uuid: '',
        password: '',
        congestion_control: 'bbr',
        udp_relay_mode: 'native',
        tls: { enabled: true, server_name: '' },
      };
      break;
    case 'hysteria2':
      settings = {
        server: '',
        server_port: 443,
        password: '',
        tls: { enabled: true, server_name: '' },
      };
      break;
    case 'anytls':
      settings = {
        server: '',
        server_port: 443,
        password: '',
        tls: { enabled: true, server_name: '' },
      };
      break;
    case 'snell':
      settings = { server: '', server_port: 443, version: 4, psk: '' };
      break;
    case 'ssh':
      settings = { server: '', server_port: 22, user: '' };
      break;
    case 'selector':
      settings = { outbounds: [], default: '' };
      break;
    case 'urltest':
      settings = { outbounds: [], url: DEFAULT_PROBE_URL, interval: '3m' };
      break;
    case 'naive':
      settings = {
        server: '',
        server_port: 443,
        username: '',
        password: '',
        tls: { enabled: true, server_name: '' },
      };
      break;
    case 'tor':
    default:
      settings = {};
      break;
  }

  return {
    protocol: normalized,
    tag: tag.trim() || `${nativeType}-out`,
    settings,
  };
}

// Accept an outbound copied directly from the official sing-box docs. The
// panel stores a shared Xray-shaped wrapper, so native { type, tag, ... }
// objects are converted to { protocol: "singbox:<type>", tag, settings }.
// The type is intentionally not checked against a panel-side allow-list:
// installed sing-box is the source of truth, so future/custom outbound types
// can be pasted in JSON mode without waiting for a panel release. Known
// removed/invalid types are still rejected by the backend validator.
// Existing panel wrappers without a native `type` are only normalized to
// lowercase and otherwise preserved.
export function normalizeOutboundJsonForPanel(
  raw: Record<string, unknown>,
): Record<string, unknown> {
  const type = typeof raw.type === 'string' ? raw.type.trim().toLowerCase() : '';
  if (type) {
    const { type: _type, tag, ...settings } = raw;
    return {
      protocol: `singbox:${type}`,
      tag,
      settings,
    };
  }

  const protocol = typeof raw.protocol === 'string' ? raw.protocol.trim().toLowerCase() : '';
  if (protocol) return { ...raw, protocol };

  return raw;
}
