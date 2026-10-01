import { z } from 'zod';

// Legacy mtg domain-fronting settings are retained in the wire schema so old
// database rows continue to parse after migration to Telemt. New UI does not
// expose them because Telemt uses its own censorship/masking implementation.
export const MtprotoDomainFrontingSchema = z.object({
  ip: z.string().optional(),
  port: z.number().int().min(0).max(65535).optional(),
  proxyProtocol: z.boolean().optional(),
});
export type MtprotoDomainFronting = z.infer<typeof MtprotoDomainFrontingSchema>;

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
  // Telemt-native per-user controls. Zero means unlimited/inherit.
  maxTcpConns: z.number().int().min(0).default(0),
  rateLimitUpBps: z.number().int().min(0).default(0),
  rateLimitDownBps: z.number().int().min(0).default(0),
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

export const TelemtModesSchema = z.object({
  classic: z.boolean().default(false),
  secure: z.boolean().default(false),
  tls: z.boolean().default(true),
});

export const MekoFixSchema = z.object({
  enabled: z.boolean().default(true),
  backend: z.enum(['auto', 'nftables', 'iptables']).default('auto'),
  synRatePerMinute: z.number().int().min(1).max(100000).default(54),
  burst: z.number().int().min(1).max(10000).default(1),
  iosBypass: z.boolean().default(true),
});

// MTProto is served by a Telemt sidecar process, not Xray. Each panel client is
// translated to one Telemt access user while preserving existing subscription
// fields and traffic/quota semantics.
export const MtprotoInboundSettingsSchema = z.object({
  fakeTlsDomain: z.string().default('www.cloudflare.com'),
  tlsDomains: z.array(z.string()).default([]),
  telemtModes: TelemtModesSchema.default({ classic: false, secure: false, tls: true }),
  mask: z.boolean().default(true),
  tlsEmulation: z.boolean().default(true),
  mekoFix: MekoFixSchema.default({
    enabled: true,
    backend: 'auto',
    synRatePerMinute: 54,
    burst: 1,
    iosBypass: true,
  }),
  clients: z.array(MtprotoClientSchema).default([]),
  proxyProtocolListener: z.boolean().optional(),
  proxyProtocolTrustedCidrs: z.array(z.string()).default(['127.0.0.0/8', '::1/128']),
  preferIp: z.enum(['prefer-ipv6', 'prefer-ipv4', 'only-ipv6', 'only-ipv4']).optional(),
  debug: z.boolean().optional(),
  routeThroughXray: z.boolean().optional(),
  outboundTag: z.string().optional(),
  routeXrayPort: z.number().int().min(0).max(65535).optional(),
  publicIpv4: z.string().optional(),
  publicIpv6: z.string().optional(),

  // Legacy mtg-only keys accepted for old saved inbounds.
  domainFronting: MtprotoDomainFrontingSchema.optional(),
  throttleMaxConnections: z.number().int().min(0).optional(),
});
export type MtprotoInboundSettings = z.infer<typeof MtprotoInboundSettingsSchema>;
