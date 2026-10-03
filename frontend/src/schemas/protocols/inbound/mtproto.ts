import { z } from 'zod';

// Legacy mtg-multi domain-fronting payload. Telemt migrates these fields to its native mask host/port;
// keep the shape in the schema only so existing saved inbounds can round-trip
// through older/newer panel versions without destructive data loss.
export const MtprotoDomainFrontingSchema = z.object({
  ip: z.string().optional(),
  port: z.number().int().min(0).max(65535).optional(),
  proxyProtocol: z.boolean().optional(),
});
export type MtprotoDomainFronting = z.infer<typeof MtprotoDomainFrontingSchema>;

// An MTProto (Telegram) client served by Telemt. The persisted secret can be a
// classic raw 32-hex secret or carry Telegram's dd/ee link prefix. The Telemt
// runtime normalises it to the shared raw secret internally. Legacy modes are
// optional; new inbounds default to FakeTLS. fakeTlsDomain is the default SNI
// used for newly generated FakeTLS links.
export const MtprotoClientSchema = z.object({
  secret: z.string().default(''),
  adTag: z
    .string()
    .regex(/^[0-9a-fA-F]{32}$/, 'pages.inbounds.form.mtgAdTagInvalid')
    .or(z.literal(''))
    .optional(),
  email: z.string().min(1),
  limitIp: z.number().int().min(0).default(0),
  totalGB: z.number().int().min(0).default(0),
  expiryTime: z.number().int().default(0),
  enable: z.boolean().default(true),
  tgId: z
    .union([z.number(), z.string()])
    .transform((v) => Number(v) || 0)
    .default(0),
  subId: z.string().default(''),
  comment: z.string().default(''),
  reset: z.number().int().min(0).default(0),
  created_at: z.number().int().optional(),
  updated_at: z.number().int().optional(),
});
export type MtprotoClient = z.infer<typeof MtprotoClientSchema>;

// MTProto inbounds are owned by one Telemt sidecar per inbound rather than by
// Xray/Sing-box, so they have no stream settings. Settings below map to native
// Telemt listener/network/access options or to the optional selected-core egress bridge.
export const MtprotoInboundSettingsSchema = z.object({
  fakeTlsDomain: z.string().default('www.cloudflare.com'),
  maskHost: z.string().optional(),
  maskPort: z.number().int().min(0).max(65535).optional(),
  maskProxyProtocol: z.union([z.literal(0), z.literal(1), z.literal(2)]).optional(),
  proxyProtocolTrustedCidrs: z.array(z.string()).optional(),
  tlsMask: z.boolean().optional(),
  allowLegacyModes: z.boolean().optional(),
  tlsEmulation: z.boolean().optional(),
  unknownSniAction: z.enum(['mask', 'drop', 'accept', 'reject_handshake']).optional(),
  clients: z.array(MtprotoClientSchema).default([]),
  proxyProtocolListener: z.boolean().optional(),
  preferIp: z.enum(['prefer-ipv6', 'prefer-ipv4', 'only-ipv6', 'only-ipv4']).optional(),
  debug: z.boolean().optional(),
  // Compatibility-only. Migrated to native mask settings when no explicit maskHost is set.
  domainFronting: MtprotoDomainFrontingSchema.optional(),
  // Telemt's per-user/global connection guard; 0 or unset disables the cap.
  throttleMaxConnections: z.number().int().min(0).optional(),
  // Route Telegram egress through the loopback SOCKS bridge owned by the selected core.
  // outboundTag optionally selects a concrete outbound/balancer; routeXrayPort
  // is allocated by the backend and is never edited manually.
  routeThroughXray: z.boolean().optional(),
  outboundTag: z.string().optional(),
  routeXrayPort: z.number().int().min(0).max(65535).optional(),
  // Public addresses are written to Telemt's listener announce_ip when their
  // address family matches the listener, so generated links advertise a
  // reachable endpoint instead of a wildcard bind address.
  publicIpv4: z.string().optional(),
  publicIpv6: z.string().optional(),
});
export type MtprotoInboundSettings = z.infer<typeof MtprotoInboundSettingsSchema>;
