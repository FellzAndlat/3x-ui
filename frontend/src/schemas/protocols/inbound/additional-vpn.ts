import { z } from 'zod';
import { NaiveClientSchema } from './naive';

export const OpenFluxTransportSchema = z.object({
  type: z.enum(['direct', 'yandex', 'vyandex', 'boards', 'mailru']),
  url: z.string().default(''),
  priority: z.number().int().min(0).max(1000).default(50),
});
export const FptnInboundSettingsSchema = z.object({
  image: z.string().default('fptnvpn/fptn-vpn-server:0.4.4'),
  hostname: z.string().default('fptn.local'),
  certificate: z.string().default(''),
  privateKey: z.string().default(''),
  certificatePEM: z.string().default(''),
  privateKeyPEM: z.string().default(''),
  metricsKey: z.string().default(''),
  mtu: z.number().int().min(576).max(9000).default(1400),
  maxSessions: z.number().int().min(1).max(1000).default(3),
  bandwidth: z.number().int().min(0).max(2000).default(0),
  detectProbing: z.boolean().default(true),
  allowedSni: z.string().default('www.bing.com'),
  adsFilter: z.boolean().default(false),
  torrentFilter: z.boolean().default(false),
  spamFilter: z.boolean().default(true),
  clients: z.array(NaiveClientSchema).default([]),
});
export const OpenFluxInboundSettingsSchema = z.object({
  context: z.string().default(''),
  transports: z
    .array(OpenFluxTransportSchema)
    .min(1)
    .max(6)
    .default([{ type: 'direct', url: '', priority: 100 }]),
  clients: z.array(NaiveClientSchema).max(1).default([]),
});
