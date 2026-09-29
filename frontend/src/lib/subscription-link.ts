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

  // The panel must always advertise the currently configured subscription URL.
  // Imported Hiddify URLs are legacy aliases kept only so already-issued links
  // continue to work; they must not override a new subscription port in the UI.
  if (settings?.subURI) return `${settings.subURI}${subId}`;

  const legacy = clientOverride || settings?.hiddifySubURIs?.[subId];
  if (legacy) return `${legacy.replace(/\/$/, '')}/${subId}/`;
  return '';
}
