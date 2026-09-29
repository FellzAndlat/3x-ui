import { describe, expect, it } from 'vitest';

import { clientSubscriptionLink } from './subscription-link';

describe('clientSubscriptionLink', () => {
  it('prefers the current configured subscription URL over a client legacy URL', () => {
    const link = clientSubscriptionLink(
      { subURI: 'https://sub.example.com:8443/sub/' },
      'client-id',
      'https://sub.example.com/sub/',
    );

    expect(link).toBe('https://sub.example.com:8443/sub/client-id');
  });

  it('prefers the current configured subscription URL over a saved Hiddify alias', () => {
    const link = clientSubscriptionLink(
      {
        subURI: 'https://sub.example.com:8443/sub/',
        hiddifySubURIs: { 'client-id': 'https://sub.example.com/legacy/' },
      },
      'client-id',
    );

    expect(link).toBe('https://sub.example.com:8443/sub/client-id');
  });

  it('falls back to the legacy URL when no current subscription URL is available', () => {
    const link = clientSubscriptionLink(
      { hiddifySubURIs: { 'client-id': 'https://sub.example.com/legacy/' } },
      'client-id',
    );

    expect(link).toBe('https://sub.example.com/legacy/client-id/');
  });
});
