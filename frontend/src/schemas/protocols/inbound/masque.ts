import { z } from 'zod';
import { NaiveClientSchema, NaiveTlsSchema } from './naive';

export const MasqueInboundSettingsSchema = z.object({
  version: z
    .array(z.union([z.literal(1), z.literal(2), z.literal(3)]))
    .min(1)
    .default([3, 2, 1]),
  path: z.string().startsWith('/').default('/.well-known/masque/ip/{target}/{ipproto}/'),
  address: z.array(z.string()).min(1).default(['172.31.255.1/24', 'fd7a:115c:a1e0::1/64']),
  advertiseRoutes: z.array(z.string()).default([]),
  mtu: z.number().int().min(1280).max(65535).default(1280),
  tls: NaiveTlsSchema.default({ enabled: true, serverName: '', certificatePath: '', keyPath: '' }),
  clients: z.array(NaiveClientSchema).default([]),
});
export type MasqueInboundSettings = z.infer<typeof MasqueInboundSettingsSchema>;
