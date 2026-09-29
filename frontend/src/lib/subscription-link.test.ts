import { describe, expect, it } from 'vitest';

import { clientSubscriptionLink } from './subscription-link';

describe('clientSubscriptionLink', () => {
  it('prefers the saved Hiddify alias over the regular subscription URL', () => {
    const link = clientSubscriptionLink(
      {
        subURI: 'https://sub.example.com:8443/sub/',
        hiddifySubURIs: { 'client-id': 'https://sub.example.com/legacy/' },
      },
      'client-id',
    );

    expect(link).toBe('https://sub.example.com/legacy/client-id/');
  });

  it('uses the regular subscription URL for clients without a Hiddify alias', () => {
    const link = clientSubscriptionLink(
      {
        subURI: 'https://sub.example.com:8443/sub/',
        hiddifySubURIs: { 'hiddify-id': 'https://sub.example.com/legacy/' },
      },
      'client-id',
    );

    expect(link).toBe('https://sub.example.com:8443/sub/client-id');
  });

  it('uses an explicit per-client Hiddify URL before the settings map', () => {
    const link = clientSubscriptionLink(
      {
        subURI: 'https://sub.example.com:8443/sub/',
        hiddifySubURIs: { 'client-id': 'https://sub.example.com/old/' },
      },
      'client-id',
      'https://sub.example.com/current/',
    );

    expect(link).toBe('https://sub.example.com/current/client-id/');
  });
});
