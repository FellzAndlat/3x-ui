import { useMutation, useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';

export type CoreType = 'xray' | 'sing-box';

export interface CoreCapabilities {
  ruleSets: boolean;
  outboundDelayProbe: boolean;
  connectionStats: boolean;
  hotInboundReload: boolean;
  clashApi: boolean;
  geoIp: boolean;
  geoSite: boolean;
  shadowTls: boolean;
}

export interface CoreCapabilitiesResponse {
  core: CoreType;
  capabilities: CoreCapabilities;
}

export interface OutboundProbeRequest {
  tag: string;
  url?: string;
  timeout?: number;
}

export interface OutboundProbeResult {
  tag: string;
  url: string;
  delay: number;
  delay2: number;
  timeout: number;
}

const coreCapabilitiesKey = ['server', 'core-capabilities'] as const;

async function fetchCoreCapabilities(): Promise<CoreCapabilitiesResponse> {
  const msg = await HttpUtil.get<CoreCapabilitiesResponse>(
    '/panel/api/server/core/capabilities',
    undefined,
    { silent: true },
  );
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch core capabilities');
  if (!msg.obj?.core || !msg.obj?.capabilities) {
    throw new Error('Invalid core capabilities response');
  }
  return msg.obj;
}

export function useCoreCapabilitiesQuery() {
  return useQuery({
    queryKey: coreCapabilitiesKey,
    queryFn: fetchCoreCapabilities,
    staleTime: 30_000,
  });
}

export function useSingBoxOutboundProbe() {
  const mutation = useMutation({
    mutationFn: async ({
      tag,
      url,
      timeout,
    }: OutboundProbeRequest): Promise<OutboundProbeResult> => {
      const payload: Record<string, string | number> = { tag };
      if (url) payload.url = url;
      if (timeout !== undefined) payload.timeout = timeout;

      const msg = await HttpUtil.post<OutboundProbeResult>(
        '/panel/api/server/singbox/outbound/check',
        payload,
      );
      if (!msg?.success) throw new Error(msg?.msg || 'Outbound probe failed');
      if (!msg.obj || typeof msg.obj.delay !== 'number') {
        throw new Error('Invalid outbound probe response');
      }
      return msg.obj;
    },
  });

  return {
    probe: (request: OutboundProbeRequest) => mutation.mutateAsync(request),
    isPending: mutation.isPending,
    error: mutation.error instanceof Error ? mutation.error.message : '',
    reset: mutation.reset,
  };
}
