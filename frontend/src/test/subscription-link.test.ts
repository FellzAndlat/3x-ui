import { describe, expect, it } from 'vitest';
import { clientSubscriptionLink } from '@/lib/subscription-link';

describe('client subscription links after Hiddify import', () => {
  const settings = {
    subURI: 'http://panel.example:2096/sub/',
    hiddifySubURIs: { imported: 'https://cdn.example.com/BackupPath123/' },
  };

  it('uses HTTPS on 443 and the backup path for imported users', () => {
    expect(clientSubscriptionLink(settings, 'imported')).toBe(
      'https://cdn.example.com/BackupPath123/imported/',
    );
  });

  it('keeps the normal port and path for other users', () => {
    expect(clientSubscriptionLink(settings, 'ordinary')).toBe(
      'http://panel.example:2096/sub/ordinary',
    );
  });
});
