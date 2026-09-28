export interface ClientSubscriptionSettings {
  subURI?: string;
  hiddifySubURIs?: Record<string, string>;
}

export function clientSubscriptionLink(
  settings: ClientSubscriptionSettings | undefined,
  subId: string | undefined,
  clientOverride?: string,
): string {
  if (!subId) return '';
  const legacy = clientOverride || settings?.hiddifySubURIs?.[subId];
  if (legacy) return `${legacy.replace(/\/$/, '')}/${subId}/`;
  return settings?.subURI ? `${settings.subURI}${subId}` : '';
}
