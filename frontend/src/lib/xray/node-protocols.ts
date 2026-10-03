import { Protocols } from '@/schemas/primitives';

/*
 * Protocols whose inbounds can live on a sub-node (the "Deploy To" set).
 * The remote panel can manage both core-native listeners and its local
 * sidecars, so eligibility is intentionally broader than either core's native
 * protocol list. Core-specific support is checked separately below and by the
 * backend.
 */
export const NODE_ELIGIBLE_PROTOCOLS: Readonly<Record<string, true>> = {
  [Protocols.VLESS]: true,
  [Protocols.VMESS]: true,
  [Protocols.TROJAN]: true,
  [Protocols.SHADOWSOCKS]: true,
  [Protocols.WIREGUARD]: true,
  [Protocols.HYSTERIA]: true,
  [Protocols.HTTP]: true,
  [Protocols.MIXED]: true,
  [Protocols.TUNNEL]: true,
  [Protocols.TUN]: true,
  [Protocols.MTPROTO]: true,
  [Protocols.AMNEZIAWG]: true,
  [Protocols.TUIC]: true,
  [Protocols.PINGTUNNEL]: true,
  [Protocols.TRUSTTUNNEL]: true,
  [Protocols.FPTN]: true,
  [Protocols.OPENFLUX]: true,
  [Protocols.NAIVE]: true,
  [Protocols.SNELL]: true,
  [Protocols.ANYTLS]: true,
  [Protocols.SHADOWTLS]: true,
  [Protocols.MIERU]: true,
  [Protocols.VK_TURN_PROXY]: true,
  [Protocols.SUDOKU]: true,
};

export interface NodeCoreInfo {
  coreType?: string | null;
  runningCore?: string | null;
}

function normalizeCore(value?: string | null): 'xray' | 'singbox' | '' {
  const normalized = (value ?? '').trim().toLowerCase().replaceAll('-', '');
  if (normalized === 'singbox') return 'singbox';
  if (normalized === 'xray') return 'xray';
  return '';
}

// Old nodes do not report coreType. Match the backend's compatibility fallback:
// a known configured core wins; otherwise a known running core is useful, and
// a completely unknown node remains Xray-compatible for legacy behaviour.
export function nodeCoreType(node: NodeCoreInfo): 'xray' | 'singbox' {
  return normalizeCore(node.coreType) || normalizeCore(node.runningCore) || 'xray';
}

export function nodeSupportsProtocol(node: NodeCoreInfo, protocol: string): boolean {
  if (!NODE_ELIGIBLE_PROTOCOLS[protocol]) return false;

  if (nodeCoreType(node) === 'singbox') {
    // Keep this in sync with backend coreSupportsInboundProtocol(). WireGuard
    // and dokodemo/tunnel are Xray-native listeners; the other entries either
    // run natively in sing-box or through panel-managed sidecars.
    return protocol !== Protocols.WIREGUARD && protocol !== Protocols.TUNNEL;
  }

  // These inbounds are generated only by sing-box. Sidecar protocols (TUIC,
  // AmneziaWG, MTProto, Mieru, PingTunnel, TrustTunnel, VK Turn and Sudoku)
  // remain valid on an Xray node because the node panel owns their processes.
  return (
    protocol !== Protocols.NAIVE &&
    protocol !== Protocols.SNELL &&
    protocol !== Protocols.ANYTLS &&
    protocol !== Protocols.SHADOWTLS
  );
}
