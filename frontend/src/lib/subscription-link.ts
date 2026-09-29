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

  // Imported Hiddify clients keep their dedicated public HTTPS/443 alias.
  // This per-client URL must win over the regular subscription listener so
  // changing subPort/subPath never rewrites Hiddify links. Ordinary clients
  // have no legacy override and continue to use the configured subURI below.
  const legacy = clientOverride || settings?.hiddifySubURIs?.[subId];
  if (legacy) return `${legacy.replace(/\/$/, '')}/${subId}/`;

  if (settings?.subURI) return `${settings.subURI}${subId}`;
  return '';
}
