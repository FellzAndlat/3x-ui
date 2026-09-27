import { HttpUtil } from '@/utils';

interface LinksResponse {
  success?: boolean;
  obj?: string[];
}

// Subscription links can include external entries, while the client endpoint
// includes every attached inbound even when the client has no subscription ID.
export async function loadClientLinks(email: string, subId?: string): Promise<string[]> {
  const paths = [
    ...(subId ? [`/panel/api/clients/subLinks/${encodeURIComponent(subId)}`] : []),
    `/panel/api/clients/links/${encodeURIComponent(email)}`,
  ];
  const responses = await Promise.allSettled(paths.map((path) => HttpUtil.get(path)));
  const links = responses.flatMap((response, index) => {
    if (response.status !== 'fulfilled') return [];
    const data = response.value as LinksResponse;
    if (!data?.success || !Array.isArray(data.obj)) return [];
    // A subscription ID can be shared by several clients. Sudoku keys belong
    // to one client, so only take those links from the email-scoped endpoint.
    const validLinks = data.obj.filter(
      (link): link is string => typeof link === 'string' && !!link,
    );
    return subId && index === 0 ? validLinks.filter((link) => !isSudokuLink(link)) : validLinks;
  });
  return [...new Set(links)];
}

export function isSudokuLink(link: string): boolean {
  return link.startsWith('sudoku://');
}
