import { z } from 'zod';

export const MieruClientSchema = z.object({
  email: z.string().min(1),
  password: z.string().default(''),
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
export type MieruClient = z.infer<typeof MieruClientSchema>;

const MieruPortEntrySchema = z
  .string()
  .trim()
  .regex(/^\d+(?:-\d+)?$/, 'Use a port or a port range such as 2012-2022')
  .superRefine((value, ctx) => {
    const [startRaw, endRaw = startRaw] = value.split('-');
    const start = Number(startRaw);
    const end = Number(endRaw);
    if (
      !Number.isInteger(start) ||
      !Number.isInteger(end) ||
      start < 1025 ||
      end > 65535 ||
      end < start
    ) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: 'Ports must be in 1025-65535',
      });
    }
  });

const MieruPortListSchema = z.array(MieruPortEntrySchema).default([]);

export const MieruInboundSettingsSchema = z.preprocess(
  (value) => {
    if (!value || typeof value !== 'object') return value;
    const raw = { ...(value as Record<string, unknown>) };
    const hasTcp = Array.isArray(raw.tcpPorts) && raw.tcpPorts.length > 0;
    const hasUdp = Array.isArray(raw.udpPorts) && raw.udpPorts.length > 0;
    if (
      !hasTcp &&
      !hasUdp &&
      Array.isArray(raw.additionalPorts) &&
      raw.additionalPorts.length > 0
    ) {
      const protocols = Array.isArray(raw.protocols) ? raw.protocols : ['TCP', 'UDP'];
      const ports = raw.additionalPorts.map((port) => String(port).trim()).filter(Boolean);
      if (protocols.includes('TCP')) raw.tcpPorts = ports;
      if (protocols.includes('UDP')) raw.udpPorts = ports;
    }
    return raw;
  },
  z.object({
    // New native mita bindings. Empty lists fall back to the legacy fields below
    // so existing inbounds continue to work unchanged.
    tcpPorts: MieruPortListSchema,
    udpPorts: MieruPortListSchema,
    // Client-side defaults used when generating mieru:// and mierus:// links.
    multiplexing: z
      .enum(['MULTIPLEXING_OFF', 'MULTIPLEXING_LOW', 'MULTIPLEXING_MIDDLE', 'MULTIPLEXING_HIGH'])
      .default('MULTIPLEXING_LOW'),
    handshakeMode: z
      .enum(['HANDSHAKE_STANDARD', 'HANDSHAKE_NO_WAIT'])
      .default('HANDSHAKE_STANDARD'),
    // Legacy compatibility fields kept for old saved inbounds.
    protocols: z
      .array(z.enum(['TCP', 'UDP']))
      .min(1)
      .optional(),
    additionalPorts: z.preprocess(
      (value) =>
        Array.isArray(value)
          ? value
              .map((item) => (typeof item === 'string' ? Number(item.trim()) : item))
              .filter((item) => Number.isFinite(item))
          : value,
      z.array(z.number().int().min(1).max(65535)).optional(),
    ),
    mtu: z.number().int().min(1280).max(1400).default(1400),
    loggingLevel: z.preprocess(
      (value) => (value === 'OFF' ? 'FATAL' : value),
      z.enum(['FATAL', 'ERROR', 'WARN', 'INFO', 'DEBUG', 'TRACE']).default('INFO'),
    ),
    userHintIsMandatory: z.boolean().default(false),
    clients: z.array(MieruClientSchema).default([]),
  }),
);
export type MieruInboundSettings = z.infer<typeof MieruInboundSettingsSchema>;
