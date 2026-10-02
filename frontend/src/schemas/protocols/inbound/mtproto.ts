import { z } from 'zod';

// Legacy mtg-multi domain-fronting payload. Telemt does not use these fields;
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
// runtime normalises it to the shared raw secret internally and enables classic,
// secure and FakeTLS handshakes simultaneously. fakeTlsDomain is the default SNI
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
// Telemt listener/network/access options or to the optional Xray egress bridge.
export const MtprotoInboundSettingsSchema = z.object({
  fakeTlsDomain: z.string().default('www.cloudflare.com'),
  clients: z.array(MtprotoClientSchema).default([]),
  proxyProtocolListener: z.boolean().optional(),
  preferIp: z.enum(['prefer-ipv6', 'prefer-ipv4', 'only-ipv6', 'only-ipv4']).optional(),
  debug: z.boolean().optional(),
  // Compatibility-only. Hidden from the Telemt form and ignored by runtime.
  domainFronting: MtprotoDomainFrontingSchema.optional(),
  // Telemt's per-user/global connection guard; 0 or unset disables the cap.
  throttleMaxConnections: z.number().int().min(0).optional(),
  // Route Telegram egress through the loopback SOCKS bridge owned by Xray.
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
