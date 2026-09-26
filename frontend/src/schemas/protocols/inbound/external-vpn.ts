import { z } from 'zod';

import { NaiveClientSchema } from './naive';

export const PingtunnelInboundSettingsSchema = z.object({
  key: z.number().int().min(0).max(2147483647).default(0),
  encrypt: z.enum(['', 'chacha20', 'aes256', 'aes128']).default('chacha20'),
  encryptKey: z.string().default(''),
  maxConn: z.number().int().min(0).default(0),
  connectTimeout: z.number().int().min(0).default(1000),
  forward: z.string().default(''),
  congestion: z.enum(['bb', 'none']).default('bb'),
  clients: z.array(z.never()).default([]),
});
export type PingtunnelInboundSettings = z.infer<typeof PingtunnelInboundSettingsSchema>;

export const TrustTunnelInboundSettingsSchema = z
  .object({
    hostname: z
      .string()
      .trim()
      .min(1)
      .refine((name) => !/[\s/:\\"]/.test(name), {
        message: 'pages.inbounds.form.trusttunnelHostnameInvalid',
      })
      .default('trusttunnel.local'),
    certificate: z.string().default(''),
    privateKey: z.string().default(''),
    clients: z.array(NaiveClientSchema).default([]),
  })
  .superRefine((settings, ctx) => {
    if (!!settings.certificate.trim() !== !!settings.privateKey.trim()) {
      ctx.addIssue({
        code: 'custom',
        path: [settings.certificate.trim() ? 'privateKey' : 'certificate'],
        message: 'pages.inbounds.form.tlsPairRequired',
      });
    }
  });
export type TrustTunnelInboundSettings = z.infer<typeof TrustTunnelInboundSettingsSchema>;
