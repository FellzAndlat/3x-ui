import { describe, it, expect, vi } from 'vitest';
import { fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import ClientFormModal from '@/pages/clients/ClientFormModal';
import type { ClientRecord, InboundOption } from '@/schemas/client';
import { renderWithProviders } from './test-utils';

// ClientFormModal reads server state via react-query (useFail2banStatusQuery),
// so it needs a QueryClientProvider on top of the shared ThemeProvider wrapper.
function renderModal({
  inbounds = [],
  mode = 'add',
  client = null,
  attachedIds = [],
}: {
  inbounds?: InboundOption[];
  mode?: 'add' | 'edit';
  client?: ClientRecord | null;
  attachedIds?: number[];
} = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithProviders(
    <QueryClientProvider client={queryClient}>
      <ClientFormModal
        open
        mode={mode}
        client={client}
        inbounds={inbounds}
        attachedIds={attachedIds}
        save={vi.fn().mockResolvedValue(null)}
        onOpenChange={() => {}}
      />
    </QueryClientProvider>,
  );
}

function openCredentialsTab() {
  const tab = Array.from(document.querySelectorAll('.ant-tabs-tab')).find(
    (t) => (t.textContent ?? '').trim() === 'Credentials',
  );
  if (!tab) throw new Error('Credentials tab not found');
  fireEvent.click(tab);
}

function tooltipIconForLabel(label: string): HTMLElement {
  const labelEl = Array.from(document.querySelectorAll('.ant-form-item-label label')).find(
    (l) => (l.textContent ?? '').trim() === label,
  );
  const item = labelEl?.closest('.ant-form-item') as HTMLElement | null;
  if (!item) throw new Error(`Form item not found for label: ${label}`);
  const tip = item.querySelector('.ant-form-item-tooltip') as HTMLElement | null;
  if (!tip) throw new Error(`No tooltip on form item: ${label}`);
  return tip;
}

describe('ClientFormModal credential tooltips', () => {
  it('shows password-based and disabled inbounds in the client picker', async () => {
    renderModal({
      inbounds: [
        { id: 1, protocol: 'shadowtls', tag: 'shadowtls-443', enable: false },
        { id: 2, protocol: 'anytls', tag: 'anytls-8443', enable: true },
      ],
    });
    const selector = await waitFor(() => {
      const label = Array.from(document.querySelectorAll('.ant-form-item-label label')).find(
        (item) => (item.textContent ?? '').includes('Attached inbounds'),
      );
      const found = label?.closest('.ant-form-item')?.querySelector('.ant-select-content');
      if (!found) throw new Error('Inbound picker not found');
      return found;
    });
    fireEvent.mouseDown(selector);
    await waitFor(() => {
      expect(document.body.textContent).toContain('shadowtls-443');
      expect(document.body.textContent).toContain('anytls-8443');
    });
  });

  it('explains which protocols consume the Password field', async () => {
    renderModal();
    openCredentialsTab();

    const tip = tooltipIconForLabel('Password');
    fireEvent.mouseEnter(tip);

    await waitFor(() => {
      expect(document.body.textContent).toContain(
        'Used by Trojan, Shadowsocks, TUIC, NaïveProxy, AnyTLS, ShadowTLS, Mieru, and TrustTunnel clients; ignored for VLESS, VMess, Hysteria, and WireGuard.',
      );
    });
  });

  it('explains that Hysteria Auth is the credential Hysteria actually uses', async () => {
    renderModal({
      inbounds: [{ id: 1, protocol: 'hysteria', enable: true }],
      mode: 'edit',
      client: { email: 'hysteria-client', enable: true },
      attachedIds: [1],
    });
    openCredentialsTab();

    const tip = tooltipIconForLabel('Hysteria Auth');
    fireEvent.mouseEnter(tip);

    await waitFor(() => {
      expect(document.body.textContent).toContain(
        'Credential used only by Hysteria clients. Trojan and Shadowsocks use the Password field instead.',
      );
    });
  });
});
