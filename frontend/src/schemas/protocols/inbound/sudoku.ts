import { z } from 'zod';

const SudokuClientBaseSchema = z.object({
  email: z.string().min(1),
  sudokuPrivateKey: z.string().default(''),
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

export const SudokuClientSchema = SudokuClientBaseSchema;
export type SudokuClient = z.infer<typeof SudokuClientSchema>;

export const SudokuHttpMaskSchema = z.object({
  disable: z.boolean().default(false),
  mode: z.enum(['legacy', 'stream', 'poll', 'auto', 'ws']).default('auto'),
  tls: z.boolean().default(false),
  host: z.string().default(''),
  pathRoot: z.string().default(''),
});

export function migrateSudokuSettings(value: unknown): unknown {
  if (!value || typeof value !== 'object') return value;
  const settings = { ...(value as Record<string, unknown>) };
  if (settings.httpmask && typeof settings.httpmask === 'object') {
    const mask = { ...(settings.httpmask as Record<string, unknown>) };
    // Older records stored this option in HTTPMask, which took precedence.
    if (typeof mask.multiplex === 'string' && mask.multiplex) {
      settings.multiplex = mask.multiplex;
    }
    delete mask.multiplex;
    settings.httpmask = mask;
  }
  return settings;
}

export const SudokuInboundSettingsSchema = z.preprocess(
  migrateSudokuSettings,
  z
    .object({
      fallbackAddress: z.string().default(''),
      key: z.string().default(''),
      aead: z.enum(['aes-128-gcm', 'chacha20-poly1305', 'none']).default('chacha20-poly1305'),
      suspiciousAction: z.enum(['fallback', 'silent']).default('silent'),
      paddingMin: z.number().int().min(0).max(100).default(5),
      paddingMax: z.number().int().min(0).max(100).default(15),
      ascii: z
        .enum(['prefer_entropy', 'prefer_ascii', 'up_ascii_down_entropy', 'up_entropy_down_ascii'])
        .default('prefer_entropy'),
      customTable: z.string().default(''),
      customTables: z.array(z.string()).default([]),
      enablePureDownlink: z.boolean().default(true),
      multiplex: z.enum(['off', 'auto', 'on']).default('off'),
      httpmask: SudokuHttpMaskSchema.default({
        disable: false,
        mode: 'auto',
        tls: false,
        host: '',
        pathRoot: '',
      }),
      clients: z.array(SudokuClientSchema).default([]),
    })
    .superRefine((settings, ctx) => {
      if (settings.paddingMax < settings.paddingMin) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['paddingMax'],
          message: 'pages.inbounds.form.sudokuPaddingRangeError',
        });
      }
      if (settings.suspiciousAction === 'fallback' && !settings.fallbackAddress.trim()) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['fallbackAddress'],
          message: 'pages.inbounds.form.sudokuFallbackRequired',
        });
      }
    }),
);
export type SudokuInboundSettings = z.infer<typeof SudokuInboundSettingsSchema>;
