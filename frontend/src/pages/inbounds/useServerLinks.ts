import { useQuery } from '@tanstack/react-query';
import { HttpUtil } from '@/utils';

export function useServerLinks(open: boolean, id?: number, protocol?: string, email?: string) {
  return useQuery({
    queryKey: ['inbound-server-links', id, protocol, email],
    enabled:
      open && !!id && !!email && ['fptn', 'openflux', 'trusttunnel'].includes(protocol ?? ''),
    queryFn: async () => {
      const response = await HttpUtil.get(
        `/panel/api/inbounds/get/${id}/links?email=${encodeURIComponent(email ?? '')}`,
      );
      if (!response?.success) throw new Error(response?.msg || 'Cannot export connection');
      return (Array.isArray(response.obj) ? response.obj : [])
        .filter((link): link is string => typeof link === 'string')
        .map((link) => ({ link, remark: protocol ?? '' }));
    },
    staleTime: 0,
  });
}
