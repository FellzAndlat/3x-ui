import { describe, expect, it } from 'vitest';
import { MasqueInboundSettingsSchema } from '@/schemas/protocols/inbound/masque';
import { createDefaultInboundSettings } from '@/lib/xray/inbound-defaults';
import { formValuesToWirePayload, rawInboundToFormValues } from '@/lib/xray/inbound-form-adapter';
import { isClientAttachableProtocol } from '@/lib/inbounds/client-attachable';
import { canEnableSniffing, canEnableStream } from '@/lib/xray/protocol-capabilities';

describe('MASQUE inbound', () => {
  it('seeds defaults and retains endpoint fields through the form adapter', () => {
    const settings = createDefaultInboundSettings('masque');
    expect(settings).toEqual(MasqueInboundSettingsSchema.parse({}));
    const values = rawInboundToFormValues({
      protocol: 'masque',
      port: 443,
      settings: JSON.stringify(settings),
    });
    const wire = formValuesToWirePayload(values);
    expect(JSON.parse(wire.settings).address).toEqual(['172.31.255.1/24', 'fd7a:115c:a1e0::1/64']);
    expect(JSON.parse(wire.settings).version).toEqual([3, 2, 1]);
    expect(isClientAttachableProtocol('masque')).toBe(true);
    expect(canEnableStream({ protocol: 'masque' })).toBe(false);
    expect(canEnableSniffing({ protocol: 'masque' })).toBe(false);
  });
});
