import {
  ALPN_OPTION,
  Address_Port_Strategy,
  MODE_OPTION,
  OutboundProtocols as Protocols,
  TLS_FLOW_CONTROL,
  USERS_SECURITY,
  UTLS_FINGERPRINT,
} from '@/schemas/primitives';
import { OutboundDomainStrategySchema } from '@/schemas/protocols/outbound';
import { SSMethodSchema } from '@/schemas/protocols/shared/shadowsocks';

// Native sing-box protocols supported by the stable 1.14.x line that don't
// have a typed Xray-compatible form. The singbox: prefix is a panel-only
// escape hatch for names that also have a legacy compatibility translator;
// it is stripped before the final sing-box config is emitted.
export const SINGBOX_NATIVE_OUTBOUND_PROTOCOLS = [
  'bridge',
  'shadowtls',
  'singbox:direct',
  'singbox:block',
  'singbox:socks',
  'singbox:http',
  'singbox:shadowsocks',
  'singbox:vmess',
  'singbox:vless',
  'singbox:trojan',
  'singbox:hysteria',
  'singbox:tuic',
  'singbox:hysteria2',
  'anytls',
  'snell',
  'tor',
  'ssh',
  'selector',
  'urltest',
  'naive',
] as const;

export const SINGBOX_NATIVE_OUTBOUND_PROTOCOL_SET = new Set<string>(
  SINGBOX_NATIVE_OUTBOUND_PROTOCOLS,
);

export const PROTOCOL_OPTIONS = [
  ...Object.values(Protocols).map((p) => ({ value: p, label: p })),
  ...SINGBOX_NATIVE_OUTBOUND_PROTOCOLS.map((p) => ({
    value: p,
    label: `${p.replace(/^singbox:/, '')} (sing-box)`,
  })),
];
export const SECURITY_OPTIONS = Object.values(USERS_SECURITY).map((v) => ({ value: v, label: v }));
export const FLOW_OPTIONS = Object.values(TLS_FLOW_CONTROL).map((v) => ({ value: v, label: v }));
export const SS_METHOD_OPTIONS = SSMethodSchema.options.map((v) => ({ value: v, label: v }));
export const MODE_OPTIONS = Object.values(MODE_OPTION).map((v) => ({ value: v, label: v }));
export const UTLS_OPTIONS = Object.values(UTLS_FINGERPRINT).map((v) => ({ value: v, label: v }));
export const ALPN_OPTIONS = Object.values(ALPN_OPTION).map((v) => ({ value: v, label: v }));
export const ADDRESS_PORT_STRATEGY_OPTIONS = Object.values(Address_Port_Strategy).map((v) => ({
  value: v,
  label: v,
}));
export const TARGET_STRATEGY_OPTIONS = OutboundDomainStrategySchema.options.map((v) => ({
  value: v,
  label: v,
}));

// canEnableMux mirrors the adapter's helper but lives here so the modal
// can show/hide the Mux section without going through the adapter.
export const MUX_PROTOCOLS = new Set<string>([
  'vmess',
  'vless',
  'trojan',
  'shadowsocks',
  'http',
  'socks',
]);

export const NETWORK_OPTIONS: { value: string; label: string }[] = [
  { value: 'tcp', label: 'RAW' },
  { value: 'kcp', label: 'mKCP' },
  { value: 'ws', label: 'WebSocket' },
  { value: 'grpc', label: 'gRPC' },
  { value: 'httpupgrade', label: 'HTTPUpgrade' },
  { value: 'xhttp', label: 'XHTTP' },
];

// The hysteria protocol is locked to its own QUIC transport: the selector
// shows only this option when the parent protocol is hysteria.
export const HYSTERIA_NETWORK_OPTION = { value: 'hysteria', label: 'Hysteria' };

// Protocols whose form schema carries a flat connect target — these all
// get the shared "server" sub-block (address + port) at the top of the
// protocol section. Wireguard has an address but no port. DNS/freedom/
// blackhole/loopback have no connect target.
export const SERVER_PROTOCOLS = new Set<string>([
  'vmess',
  'vless',
  'trojan',
  'shadowsocks',
  'socks',
  'http',
  'hysteria',
]);
