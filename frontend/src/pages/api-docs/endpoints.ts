export type {
  HttpMethod,
  ParamLocation,
  ParamType,
  EndpointParam,
  Endpoint,
  SubscriptionHeader,
  Section,
} from './endpoints.base.ts';

import { sections as baseSections } from './endpoints.base.ts';
import type { Section } from './endpoints.base.ts';

const singBoxOutboundDiagnostics: Section = {
  id: 'singbox-outbound-diagnostics',
  title: 'sing-box Outbound Diagnostics',
  description:
    'Measure reachability and latency of an outbound in the running sing-box instance through its loopback-only Clash API.',
  endpoints: [
    {
      method: 'POST',
      path: '/panel/api/server/singbox/outbound/check',
      summary: 'Test a running sing-box outbound by tag and return its measured delay.',
      params: [
        {
          name: 'tag',
          in: 'body (form)',
          type: 'string',
          desc: 'Outbound tag to test (required).',
        },
        {
          name: 'url',
          in: 'body (form)',
          type: 'string',
          desc: 'HTTP(S) URL used by sing-box for the probe.',
          optional: true,
          defaultValue: 'https://www.gstatic.com/generate_204',
        },
        {
          name: 'timeout',
          in: 'body (form)',
          type: 'integer',
          desc: 'Probe timeout in milliseconds. Allowed range: 1-30000.',
          optional: true,
          defaultValue: 5000,
        },
      ],
      response:
        '{\n  "success": true,\n  "obj": {\n    "tag": "proxy",\n    "url": "https://www.gstatic.com/generate_204",\n    "delay": 82,\n    "delay2": 0,\n    "timeout": 5000\n  }\n}',
      errorResponse:
        '{\n  "success": false,\n  "msg": "sing-box is not running | outbound tag is required | timeout must be between 1 and 30000 milliseconds | <Clash API error>"\n}',
    },
  ],
};

export const sections: readonly Section[] = [...baseSections, singBoxOutboundDiagnostics];
