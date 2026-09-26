import { z } from 'zod';

export const ANYTLS_DEFAULT_PADDING_SCHEME = [
  'stop=8',
  '0=30-30',
  '1=100-400',
  '2=400-500,c,500-1000,c,500-1000,c,500-1000,c,500-1000',
  '3=9-9,500-1000',
  '4=500-1000',
  '5=500-1000',
  '6=500-1000',
  '7=500-1000',
];

export const AnyTlsClientSchema = z.object({
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
export type AnyTlsClient = z.infer<typeof AnyTlsClientSchema>;

export const AnyTlsServerTlsSchema = z.object({
  enabled: z.boolean().default(true),
  serverName: z.string().default(''),
  certificatePath: z.string().default(''),
  keyPath: z.string().default(''),
});
export type AnyTlsServerTls = z.infer<typeof AnyTlsServerTlsSchema>;

export const AnyTlsInboundSettingsSchema = z
  .object({
    paddingScheme: z.array(z.string()).default(ANYTLS_DEFAULT_PADDING_SCHEME),
    tls: AnyTlsServerTlsSchema.default({
      enabled: true,
      serverName: '',
      certificatePath: '',
      keyPath: '',
    }),
    clients: z.array(AnyTlsClientSchema).default([]),
  })
  .superRefine((settings, ctx) => {
    const cert = settings.tls.certificatePath.trim();
    const key = settings.tls.keyPath.trim();
    if (!!cert !== !!key) {
      ctx.addIssue({
        code: 'custom',
        path: ['tls', cert ? 'keyPath' : 'certificatePath'],
        message: 'pages.inbounds.form.tlsPairRequired',
      });
    }
  });
export type AnyTlsInboundSettings = z.infer<typeof AnyTlsInboundSettingsSchema>;
