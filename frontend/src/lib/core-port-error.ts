import i18next from 'i18next';

// sing-box and Xray can report bind failures after the restart request returns.
// Keep the same explanation for an API error and the polled core status.
export function corePortBindError(raw: string): string | null {
  const error = raw.replace(/\x1b\[[0-9;]*m/g, '');
  if (!/bind:\s*address already in use/i.test(error)) return null;
  const port = error.match(/\b(?:tcp|udp)(?:4|6)?\s+\S+:(\d{1,5})\b/i)?.[1];
  if (!port) return null;
  return i18next.t('pages.index.corePortInUse', { port });
}
