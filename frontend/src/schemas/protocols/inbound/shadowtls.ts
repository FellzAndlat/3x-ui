import { z } from 'zod';

export const ShadowTlsClientSchema = z.object({
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
export type ShadowTlsClient = z.infer<typeof ShadowTlsClientSchema>;

export const ShadowTlsInboundSettingsSchema = z.object({
  version: z.literal(3).default(3),
  handshake: z.object({
    server: z
      .string()
      .default('cloudflare.com')
      .transform((value) => value.trim() || 'cloudflare.com'),
    serverPort: z.number().int().min(1).max(65535).default(443),
  }),
  strictMode: z.boolean().default(false),
  wildcardSni: z.enum(['off', 'authed', 'all']).default('off'),
  clients: z.array(ShadowTlsClientSchema).default([]),
});
export type ShadowTlsInboundSettings = z.infer<typeof ShadowTlsInboundSettingsSchema>;
