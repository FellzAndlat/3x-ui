import { z } from 'zod';
import { NaiveClientSchema } from './naive';

export const SnellInboundSettingsSchema = z.object({
  version: z.union([z.literal(5), z.literal(6)]).default(6),
  psk: z.string().default(''),
  mode: z.enum(['default', 'unshaped']).default('default'),
  obfsMode: z.enum(['none', 'http']).default('none'),
  obfsHost: z.string().default('bing.com'),
  clients: z.array(NaiveClientSchema).default([]),
});
export type SnellInboundSettings = z.infer<typeof SnellInboundSettingsSchema>;
